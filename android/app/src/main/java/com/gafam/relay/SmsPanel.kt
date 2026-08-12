package com.gafam.relay

import android.Manifest
import android.content.ClipData
import android.content.Context
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.ContactsContract
import android.telephony.SmsManager
import android.view.Gravity
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat
import kotlin.concurrent.thread

object SmsPanel {

    data class SmsMsg(
        val id: Long,
        val address: String,
        val body: String,
        val date: Long,
        val type: Int
    )

    data class Conversation(
        val address: String,
        val name: String,
        val lastDate: Long,
        val lastBody: String,
        val count: Int,
        val lastType: Int
    )

    private val contactCache = mutableMapOf<String, String>()
    private var convListLayout: LinearLayout? = null
    private var chatDetailLayout: LinearLayout? = null
    private var mainContainer: LinearLayout? = null
    private var ctxRef: Context? = null

    fun create(ctx: Context): View {
        ctxRef = ctx
        val root = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(0xFF111111.toInt())
        }
        mainContainer = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.MATCH_PARENT
            )
        }
        root.addView(mainContainer)
        convListLayout = buildConversationListLayout(ctx)
        chatDetailLayout = buildChatDetailLayout(ctx)
        showConversations()
        return root
    }

    private fun showConversations() {
        val mc = mainContainer ?: return
        mc.removeAllViews()
        mc.addView(convListLayout)
        val convScroll = convListLayout?.getChildAt(1) as? ScrollView
        val convContainer = convScroll?.getChildAt(0) as? LinearLayout
        convContainer?.let { loadConversations(it) }
    }

    private fun showChat(conv: Conversation) {
        val ctx = ctxRef ?: return
        val mc = mainContainer ?: return
        val chatLayout = chatDetailLayout ?: return
        val refs = chatLayout.tag as? ChatRefs ?: return

        refs.titleText.text = conv.name
        refs.subtitleText.text = conv.address
        refs.backBtn.setOnClickListener { showConversations() }

        refs.composeBar.removeAllViews()
        val freshCompose = buildComposeBar(ctx) { recipient, body ->
            sendSms(ctx, recipient, body)
        }
        refs.composeBar.addView(freshCompose)
        (freshCompose.getChildAt(0) as? EditText)?.setText(conv.address)

        // Delete button
        refs.deleteBtn.setOnClickListener {
            android.app.AlertDialog.Builder(ctx)
                .setTitle("Delete conversation")
                .setMessage("Delete all messages with ${conv.name}?")
                .setPositiveButton("Delete") { _, _ ->
                    deleteConversation(ctx, conv.address)
                    showConversations()
                }
                .setNegativeButton("Cancel", null)
                .show()
        }

        loadMessages(conv.address, refs.msgContainer)
        mc.removeAllViews()
        mc.addView(chatLayout)
        refs.scroll.post { refs.scroll.fullScroll(View.FOCUS_DOWN) }
    }

    // ── Chat detail layout ──

    private class ChatRefs(
        val titleText: TextView,
        val subtitleText: TextView,
        val msgContainer: LinearLayout,
        val scroll: ScrollView,
        val composeBar: LinearLayout,
        val backBtn: TextView,
        val deleteBtn: TextView
    )

    private fun buildChatDetailLayout(ctx: Context): LinearLayout {
        val layout = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }

        val titleRow = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(4, 6, 4, 6)
            setBackgroundColor(0xFF181818.toInt())
            gravity = Gravity.CENTER_VERTICAL
        }
        val backBtn = buildTextBtn(ctx, "\u2190") {}
        val titleBlock = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            setPadding(8, 4, 8, 4)
        }
        val titleText = TextView(ctx).apply {
            setTextColor(0xFFDDDDDD.toInt())
            textSize = 15f
            maxLines = 1
        }
        val subtitleText = TextView(ctx).apply {
            setTextColor(0xFF888888.toInt())
            textSize = 11f
            maxLines = 1
        }
        titleBlock.addView(titleText)
        titleBlock.addView(subtitleText)

        val deleteBtn = buildTextBtn(ctx, "\uD83D\uDDD1") {}

        titleRow.addView(backBtn)
        titleRow.addView(titleBlock)
        titleRow.addView(deleteBtn)

        val chatScroll = ScrollView(ctx)
        val msgContainer = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, 8, 0, 8)
        }
        chatScroll.addView(msgContainer)
        val composeBar = LinearLayout(ctx)

        layout.addView(titleRow)
        layout.addView(chatScroll, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f
        ))
        layout.addView(composeBar)

        layout.tag = ChatRefs(titleText, subtitleText, msgContainer, chatScroll, composeBar, backBtn, deleteBtn)
        return layout
    }

    // ── Conversation list layout ──

    private fun buildConversationListLayout(ctx: Context): LinearLayout {
        val layout = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }

        val header = buildHeader(ctx, "\uD83D\uDCAC  Messages")
        val convScroll = ScrollView(ctx)
        val convContainer = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }
        convScroll.addView(convContainer)

        val newBtn = buildBtn(ctx, "+ New message") {
            showNewMessageDialog(ctx) { recipient ->
                showChat(Conversation(recipient, recipient, 0, "", 0, 1))
            }
        }

        layout.addView(header)
        layout.addView(convScroll, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f
        ))
        layout.addView(newBtn)
        return layout
    }

    private fun showNewMessageDialog(ctx: Context, cb: (String) -> Unit) {
        val input = EditText(ctx).apply {
            hint = "Phone number"
            setHintTextColor(0xFF555555.toInt())
            setTextColor(0xFFDDDDDD.toInt())
            setBackgroundColor(0xFF1E1E1E.toInt())
            setPadding(16, 14, 16, 14)
            textSize = 14f
            inputType = android.text.InputType.TYPE_CLASS_PHONE
        }
        android.app.AlertDialog.Builder(ctx)
            .setTitle("New SMS")
            .setView(input)
            .setPositiveButton("OK") { _, _ ->
                val t = input.text.toString().trim()
                if (t.isNotBlank()) cb(t)
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    // ── Load conversations ──

    private fun loadConversations(container: LinearLayout) {
        val ctx = ctxRef ?: return
        container.removeAllViews()
        val loading = TextView(ctx).apply {
            text = "Loading..."
            setTextColor(0xFF666666.toInt())
            setPadding(16, 32, 16, 16)
            gravity = Gravity.CENTER
        }
        container.addView(loading)
        thread(name = "gafam-load-convs", isDaemon = true) {
            val convs = buildConversations(ctx)
            (ctx as? android.app.Activity)?.runOnUiThread {
                renderConversations(ctx, convs, container)
            }
        }
    }

    private fun buildConversations(ctx: Context): List<Conversation> {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED) return emptyList()

        val map = mutableMapOf<String, MutableList<SmsMsg>>()
        ctx.contentResolver.query(
            Uri.parse("content://sms"),
            arrayOf("_id", "address", "body", "date", "type"),
            null, null, "date DESC"
        )?.use {
            val idIdx = it.getColumnIndex("_id")
            val addrIdx = it.getColumnIndex("address")
            val bodyIdx = it.getColumnIndex("body")
            val dateIdx = it.getColumnIndex("date")
            val typeIdx = it.getColumnIndex("type")
            while (it.moveToNext()) {
                val addr = it.getString(addrIdx) ?: ""
                if (addr.isBlank()) continue
                val msg = SmsMsg(it.getLong(idIdx), addr,
                    it.getString(bodyIdx) ?: "", it.getLong(dateIdx), it.getInt(typeIdx))
                val norm = addr.filter { c -> c.isDigit() }.takeLast(9)
                if (norm.length >= 7) map.getOrPut(norm) { mutableListOf() }.add(msg)
            }
        }
        return map.map { (_, msgs) ->
            val last = msgs.first()
            Conversation(last.address, lookupName(ctx, last.address),
                last.date, last.body, msgs.size, last.type)
        }.sortedByDescending { it.lastDate }
    }

    private fun renderConversations(ctx: Context, convs: List<Conversation>, container: LinearLayout) {
        container.removeAllViews()
        val divColor = 0xFF222222.toInt()
        if (convs.isEmpty()) {
            container.addView(emptyView(ctx, "No conversations yet"))
            return
        }
        for (conv in convs) {
            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.HORIZONTAL
                setPadding(12, 11, 12, 11)
                gravity = Gravity.CENTER_VERTICAL
                setOnClickListener { showChat(conv) }
                setOnLongClickListener {
                    android.app.AlertDialog.Builder(ctx)
                        .setTitle("Delete conversation")
                        .setMessage("Delete all messages with ${conv.name}?")
                        .setPositiveButton("Delete") { _, _ ->
                            deleteConversation(ctx, conv.address)
                            loadConversations(container)
                        }
                        .setNegativeButton("Cancel", null)
                        .show()
                    true
                }
            }

            val avatar = ContactsPanel.buildAvatar(ctx,
                conv.name.take(1).uppercase(), 0xFF555555.toInt())
            row.addView(avatar)

            val col = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
                setPadding(12, 0, 8, 0)
            }
            val prefix = if (conv.lastType == 2) "You: " else ""
            col.addView(TextView(ctx).apply {
                text = conv.name
                setTextColor(0xFFDDDDDD.toInt()); textSize = 15f; maxLines = 1
            })
            col.addView(TextView(ctx).apply {
                text = (prefix + conv.lastBody).take(60)
                setTextColor(0xFF888888.toInt()); textSize = 12f; maxLines = 1
            })
            val timeLbl = TextView(ctx).apply {
                text = formatDateShort(conv.lastDate)
                setTextColor(0xFF666666.toInt()); textSize = 11f
            }
            row.addView(col)
            row.addView(timeLbl)
            container.addView(row)
            container.addView(View(ctx).apply {
                layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1)
                setBackgroundColor(divColor)
            })
        }
    }

    // ── Load messages ──

    private fun loadMessages(address: String, container: LinearLayout) {
        val ctx = ctxRef ?: return
        container.removeAllViews()
        container.addView(TextView(ctx).apply {
            text = "Loading..."; setTextColor(0xFF666666.toInt())
            setPadding(0, 32, 0, 0); gravity = Gravity.CENTER
        })
        thread(name = "gafam-load-msgs", isDaemon = true) {
            val msgs = readMessages(ctx, address)
            (ctx as? android.app.Activity)?.runOnUiThread {
                renderMessages(ctx, msgs, container)
                container.post {
                    (container.parent as? ScrollView)?.fullScroll(View.FOCUS_DOWN)
                }
            }
        }
    }

    private fun readMessages(ctx: Context, address: String): List<SmsMsg> {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED) return emptyList()
        val list = mutableListOf<SmsMsg>()
        ctx.contentResolver.query(
            Uri.parse("content://sms"),
            arrayOf("_id", "address", "body", "date", "type"),
            null, null, "date ASC"
        )?.use {
            val idI = it.getColumnIndex("_id"); val aI = it.getColumnIndex("address")
            val bI = it.getColumnIndex("body"); val dI = it.getColumnIndex("date")
            val tI = it.getColumnIndex("type")
            while (it.moveToNext()) {
                val a = it.getString(aI) ?: ""
                if (phonesMatch(a, address)) list.add(SmsMsg(
                    it.getLong(idI), a, it.getString(bI) ?: "", it.getLong(dI), it.getInt(tI)))
            }
        }
        return list
    }

    private fun phonesMatch(a: String, b: String): Boolean {
        val d = { s: String -> s.filter { it.isDigit() }.takeLast(9) }
        return d(a).length >= 7 && d(b).length >= 7 && d(a) == d(b)
    }

    private fun renderMessages(ctx: Context, msgs: List<SmsMsg>, container: LinearLayout) {
        container.removeAllViews()
        if (msgs.isEmpty()) {
            container.addView(emptyView(ctx, "No messages yet"))
            return
        }
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val bubbleIn = buildBubbleDrawable(0xFF2A2A2A.toInt(), dp(12))
        val bubbleOut = buildBubbleDrawable(0xFF1A3A5A.toInt(), dp(12))
        val maxW = (ctx.resources.displayMetrics.widthPixels * 0.76).toInt()

        var lastDateBlock: String? = null
        for (msg in msgs) {
            val dateBlock = formatDateBlock(msg.date)
            if (dateBlock != lastDateBlock) {
                lastDateBlock = dateBlock
                val header = TextView(ctx).apply {
                    text = dateBlock
                    setTextColor(0xFF666666.toInt()); textSize = 11f
                    gravity = Gravity.CENTER; setPadding(0, 16, 0, 8)
                }
                container.addView(header)
            }

            val isOut = msg.type == 2
            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(10, 3, 10, 3)
                gravity = if (isOut) Gravity.END else Gravity.START
            }

            val bubble = TextView(ctx).apply {
                text = msg.body
                setTextColor(0xFFDDDDDD.toInt()); textSize = 14f
                setPadding(dp(14), dp(10), dp(14), dp(10))
                background = if (isOut) bubbleOut else bubbleIn
                maxWidth = maxW
            }

            // Long press → copy message
            bubble.setOnLongClickListener {
                val clip = ClipData.newPlainText("sms", msg.body)
                (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                    .setPrimaryClip(clip)
                android.widget.Toast.makeText(ctx, "Copied", android.widget.Toast.LENGTH_SHORT).show()
                true
            }

            val timeLbl = TextView(ctx).apply {
                text = formatTime(msg.date)
                setTextColor(0xFF555555.toInt()); textSize = 10f
                gravity = if (isOut) Gravity.END else Gravity.START
                setPadding(dp(8), dp(2), dp(8), dp(2))
            }

            row.addView(bubble)
            row.addView(timeLbl)
            container.addView(row)
        }
    }

    private fun buildBubbleDrawable(color: Int, radius: Int): android.graphics.drawable.Drawable {
        return android.graphics.drawable.GradientDrawable().apply {
            setColor(color)
            cornerRadius = radius.toFloat()
        }
    }

    // ── Delete conversation ──

    private fun deleteConversation(ctx: Context, address: String) {
        if (ContextCompat.checkSelfPermission(ctx, "android.permission.WRITE_SMS")
            != PackageManager.PERMISSION_GRANTED) {
            android.widget.Toast.makeText(ctx, "Need WRITE_SMS permission (set as default SMS app)",
                android.widget.Toast.LENGTH_LONG).show()
            return
        }
        try {
            val deleted = ctx.contentResolver.delete(
                Uri.parse("content://sms"),
                "address = ?", arrayOf(address)
            )
            android.widget.Toast.makeText(ctx, "Deleted $deleted messages",
                android.widget.Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            android.widget.Toast.makeText(ctx, "Delete failed: ${e.message}",
                android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    // ── Compose bar ──

    private fun buildComposeBar(ctx: Context, onSend: (String, String) -> Unit): LinearLayout {
        val bar = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(0xFF181818.toInt())
            setPadding(8, 6, 8, 6)
        }
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }

        val recipientInput = EditText(ctx).apply {
            hint = "Recipient"
            setHintTextColor(0xFF555555.toInt()); setTextColor(0xFFDDDDDD.toInt())
            setBackgroundColor(0xFF1E1E1E.toInt()); setPadding(dp(12), dp(8), dp(12), dp(8))
            textSize = 13f; maxLines = 1; inputType = android.text.InputType.TYPE_CLASS_PHONE
        }
        val inputRow = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL
            setPadding(0, dp(4), 0, 0)
        }
        val bodyInput = EditText(ctx).apply {
            hint = "Message"
            setHintTextColor(0xFF555555.toInt()); setTextColor(0xFFDDDDDD.toInt())
            setBackgroundColor(0xFF1E1E1E.toInt()); setPadding(dp(12), dp(10), dp(12), dp(10))
            textSize = 14f; maxLines = 4
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            imeOptions = EditorInfo.IME_ACTION_SEND
        }
        val sendBtn = buildBtn(ctx, "Send") {
            val r = recipientInput.text.toString().trim()
            val b = bodyInput.text.toString().trim()
            if (r.isNotEmpty() && b.isNotEmpty()) {
                onSend(r, b)
                bodyInput.text.clear()
            }
        }
        inputRow.addView(bodyInput)
        inputRow.addView(sendBtn)
        bar.addView(recipientInput)
        bar.addView(inputRow)
        return bar
    }

    private fun sendSms(ctx: Context, recipient: String, body: String) {
        try {
            val mgr = SmsManager.getDefault()
            val parts = mgr.divideMessage(body)
            if (parts.size > 1) mgr.sendMultipartTextMessage(recipient, null, parts, null, null)
            else mgr.sendTextMessage(recipient, null, body, null, null)
            android.widget.Toast.makeText(ctx, "Sent", android.widget.Toast.LENGTH_SHORT).show()
            SmsHistorySync.syncAsync(ctx, force = true)
        } catch (e: Exception) {
            android.widget.Toast.makeText(ctx, "Error: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    // ── Contact lookup ──

    private fun lookupName(ctx: Context, phone: String): String {
        val norm = phone.filter { it.isDigit() }.takeLast(9)
        if (norm.length < 7) return phone
        contactCache[norm]?.let { return it }

        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_CONTACTS)
            == PackageManager.PERMISSION_GRANTED) {
            try {
                val uri = android.net.Uri.withAppendedPath(
                    ContactsContract.PhoneLookup.CONTENT_FILTER_URI,
                    android.net.Uri.encode(phone))
                ctx.contentResolver.query(uri,
                    arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME),
                    null, null, null)?.use {
                    if (it.moveToFirst()) {
                        val name = it.getString(it.getColumnIndex(
                            ContactsContract.PhoneLookup.DISPLAY_NAME))
                        if (name != null) { contactCache[norm] = name; return name }
                    }
                }
            } catch (_: Exception) {}
        }
        contactCache[norm] = phone
        return phone
    }

    // ── UI helpers ──

    private fun buildHeader(ctx: Context, title: String): LinearLayout {
        return LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(14, 10, 14, 10)
            setBackgroundColor(0xFF181818.toInt())
            gravity = Gravity.CENTER_VERTICAL
            addView(TextView(ctx).apply {
                text = title; setTextColor(0xFFDDDDDD.toInt()); textSize = 16f
            })
        }
    }

    private fun buildBtn(ctx: Context, text: String, onClick: () -> Unit): TextView {
        return TextView(ctx).apply {
            this.text = text; setTextColor(0xFF111111.toInt()); textSize = 13f
            setPadding(14, 10, 14, 10); gravity = Gravity.CENTER
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(0xFFAAAAAA.toInt())
                cornerRadius = 6f * ctx.resources.displayMetrics.density
            }
            setOnClickListener { onClick() }
        }
    }

    private fun buildTextBtn(ctx: Context, text: String, onClick: () -> Unit): TextView {
        return TextView(ctx).apply {
            this.text = text; setTextColor(0xFFCCCCCC.toInt()); textSize = 16f
            setPadding(12, 8, 12, 8); gravity = Gravity.CENTER
            setOnClickListener { onClick() }
        }
    }

    private fun emptyView(ctx: Context, msg: String): TextView {
        return TextView(ctx).apply {
            text = msg; setTextColor(0xFF666666.toInt()); textSize = 14f
            gravity = Gravity.CENTER; setPadding(0, 64, 0, 0)
        }
    }

    private fun formatDateShort(ts: Long): String {
        if (ts == 0L) return ""
        val diff = System.currentTimeMillis() - ts
        return when {
            diff < 60_000 -> "now"
            diff < 3_600_000 -> "${diff / 60_000}m"
            diff < 86_400_000 -> "${diff / 3_600_000}h"
            diff < 172_800_000 -> "yest."
            else -> {
                val c = java.util.Calendar.getInstance().apply { timeInMillis = ts }
                "${"%02d".format(c.get(java.util.Calendar.DAY_OF_MONTH))}/" +
                    "${"%02d".format(c.get(java.util.Calendar.MONTH) + 1)}"
            }
        }
    }

    private fun formatDateBlock(ts: Long): String {
        val c = java.util.Calendar.getInstance().apply { timeInMillis = ts }
        val now = java.util.Calendar.getInstance()
        val days = mapOf(
            java.util.Calendar.SUNDAY to "Sun", java.util.Calendar.MONDAY to "Mon",
            java.util.Calendar.TUESDAY to "Tue", java.util.Calendar.WEDNESDAY to "Wed",
            java.util.Calendar.THURSDAY to "Thu", java.util.Calendar.FRIDAY to "Fri",
            java.util.Calendar.SATURDAY to "Sat"
        )
        val sameDay = c.get(java.util.Calendar.YEAR) == now.get(java.util.Calendar.YEAR) &&
            c.get(java.util.Calendar.DAY_OF_YEAR) == now.get(java.util.Calendar.DAY_OF_YEAR)
        return if (sameDay) "Today"
        else {
            val d = c.get(java.util.Calendar.DAY_OF_MONTH)
            val m = c.get(java.util.Calendar.MONTH) + 1
            val y = c.get(java.util.Calendar.YEAR)
            "${days[c.get(java.util.Calendar.DAY_OF_WEEK)]} ${"%02d".format(d)}/${"%02d".format(m)}/$y"
        }
    }

    private fun formatTime(ts: Long): String {
        val c = java.util.Calendar.getInstance().apply { timeInMillis = ts }
        return "${"%02d".format(c.get(java.util.Calendar.HOUR_OF_DAY))}:" +
            "${"%02d".format(c.get(java.util.Calendar.MINUTE))}"
    }
}
