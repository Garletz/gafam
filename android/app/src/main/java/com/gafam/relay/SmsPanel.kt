package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.ContactsContract
import android.telephony.SmsManager
import android.view.Gravity
import android.view.View
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat
import kotlin.concurrent.thread
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

object SmsPanel {

    data class SmsMsg(val id: Long, val address: String, val body: String, val date: Long, val type: Int)
    data class Conversation(val address: String, val name: String, val lastDate: Long, val lastBody: String, val count: Int, val lastType: Int)

    private val contactCache = mutableMapOf<String, String>()
    private var convListLayout: LinearLayout? = null
    private var convListScroll: ScrollView? = null
    private var convListContainer: LinearLayout? = null
    private var chatDetailLayout: LinearLayout? = null
    private var mainContainer: LinearLayout? = null
    private var ctxRef: Context? = null
    private var currentConv: Conversation? = null
    private var draftTimer: java.util.Timer? = null
    private var draftPollTimer: java.util.Timer? = null
    @Volatile private var draftDirty = false
    @Volatile private var draftLoading = false
    private var lastDraftVersion: String? = null
    @Volatile private var panelVisible = false
    private var composeIsNew = false
    private var currentBodyEdit: EditText? = null
    private var smsRefreshReceiver: android.content.BroadcastReceiver? = null
    private var refreshCurrentChat: (() -> Unit)? = null

    fun create(ctx: Context): View {
        ctxRef = ctx
        val root = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL; setBackgroundColor(0xFF111111.toInt())
        }
        mainContainer = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.MATCH_PARENT)
        }
        root.addView(mainContainer)
        convListLayout = buildConversationListLayout(ctx)
        chatDetailLayout = buildChatDetailLayout(ctx)
        showConversations()

        smsRefreshReceiver = object : android.content.BroadcastReceiver() {
            override fun onReceive(context: android.content.Context?, intent: android.content.Intent?) {
                refreshCurrentChat?.invoke()
            }
        }
        val filter = android.content.IntentFilter("com.gafam.relay.NEW_SMS")
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            ctx.registerReceiver(smsRefreshReceiver, filter, android.content.Context.RECEIVER_NOT_EXPORTED)
        } else {
            ctx.registerReceiver(smsRefreshReceiver, filter)
        }
        return root
    }

    fun onPanelShown() {
        panelVisible = true
        currentConv?.let { conv ->
            val ctx = ctxRef ?: return
            val refs = chatDetailLayout?.tag as? ChatRefs ?: return
            startDraftPoll(ctx, conv, currentBodyEdit, refs)
        }
    }

    fun onPanelHidden() {
        panelVisible = false
        draftTimer?.cancel(); draftTimer = null
        draftPollTimer?.cancel(); draftPollTimer = null
    }

    fun onBackPressed(): Boolean {
        if (currentConv != null) {
            showConversations()
            return true
        }
        return false
    }

    fun onDestroy() {
        draftTimer?.cancel(); draftPollTimer?.cancel()
        smsRefreshReceiver?.let { ctxRef?.unregisterReceiver(it) }
        smsRefreshReceiver = null
        refreshCurrentChat = null
    }

    private fun showConversations() {
        currentConv = null; draftPollTimer?.cancel(); refreshCurrentChat = null
        val mc = mainContainer ?: return; mc.removeAllViews(); mc.addView(convListLayout)
        convListContainer?.let { loadConversations(it) }
    }

    private fun showChat(conv: Conversation) {
        val ctx = ctxRef ?: return; val mc = mainContainer ?: return
        val cl = chatDetailLayout ?: return; val refs = cl.tag as? ChatRefs ?: return
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }

        draftTimer?.cancel(); draftTimer = null
        draftPollTimer?.cancel(); draftPollTimer = null
        lastDraftVersion = null
        currentConv = conv

        refs.titleText.text = conv.name
        refs.subtitleText.text = conv.address
        refs.backBtn.setOnClickListener { showConversations() }

        refs.deleteBtn.setOnClickListener {
            android.app.AlertDialog.Builder(ctx)
                .setTitle("Delete conversation").setMessage("Delete all messages with ${conv.name}?")
                .setPositiveButton("Delete") { _, _ -> deleteConversation(ctx, conv.address); showConversations() }
                .setNegativeButton("Cancel", null).show()
        }

        refs.composeBar.removeAllViews()
        composeIsNew = conv.address == conv.name && conv.count == 0

        // Auto-refresh on incoming SMS
        refreshCurrentChat = {
            (ctx as? android.app.Activity)?.runOnUiThread {
                if (currentConv == conv) loadMessages(conv.address, refs.msgContainer)
            }
        }

        val (compBar, bodyEdit) = buildComposeBar(ctx, conv.address, composeIsNew) { recipient, body ->
            sendSms(ctx, recipient, body) {
                loadMessages(conv.address, refs.msgContainer)
                if (composeIsNew) {
                    composeIsNew = false
                    rebuildComposeBar(ctx, conv.address, refs, currentBodyEdit)
                }
            }
        }
        currentBodyEdit = bodyEdit
        refs.composeBar.addView(compBar)

        loadDraftFromVpc(ctx, conv.address) { draftBody, updatedAt ->
            if (currentConv?.address == conv.address) {
                lastDraftVersion = updatedAt
                (ctx as? android.app.Activity)?.runOnUiThread {
                    draftLoading = true
                    bodyEdit?.setText(draftBody)
                    if (draftBody.isNotEmpty()) bodyEdit?.setSelection(draftBody.length)
                    draftLoading = false
                    refs.syncDot.setTextColor(0xFF34A853.toInt())
                }
            }
        }

        bodyEdit?.tag?.let { old ->
            try { bodyEdit?.removeTextChangedListener(old as android.text.TextWatcher) } catch (_: Exception) {}
        }

        val watcher = object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) {
                if (draftLoading) return
                draftDirty = true
                draftTimer?.cancel(); draftTimer = java.util.Timer(true)
                draftTimer?.schedule(object : java.util.TimerTask() {
                    override fun run() {
                        saveDraftToVpc(ctx, conv.address, s?.toString() ?: "") {
                            draftDirty = false
                        }
                    }
                }, 300)
            }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        }
        bodyEdit?.tag = watcher
        bodyEdit?.addTextChangedListener(watcher)

        if (panelVisible) {
            startDraftPoll(ctx, conv, bodyEdit, refs)
        }

        loadMessages(conv.address, refs.msgContainer)
        mc.removeAllViews(); mc.addView(cl)
        refs.scroll.post { refs.scroll.fullScroll(View.FOCUS_DOWN) }
    }

    private fun startDraftPoll(ctx: Context, conv: Conversation, bodyEdit: EditText?, refs: ChatRefs) {
        draftPollTimer?.cancel(); draftPollTimer = java.util.Timer(true)
        draftPollTimer?.schedule(object : java.util.TimerTask() {
            override fun run() {
                if (currentConv?.address != conv.address) return
                if (draftDirty) return
                loadDraftFromVpc(ctx, conv.address) { draftBody, updatedAt ->
                    if (currentConv?.address == conv.address) {
                        if (updatedAt.isEmpty() || updatedAt == lastDraftVersion) return@loadDraftFromVpc
                        lastDraftVersion = updatedAt
                        val current = bodyEdit?.text?.toString() ?: ""
                        if (current != draftBody) {
                            (ctx as? android.app.Activity)?.runOnUiThread {
                                if (currentConv?.address == conv.address) {
                                    draftLoading = true
                                    bodyEdit?.setText(draftBody)
                                    if (draftBody.isNotEmpty()) bodyEdit?.setSelection(draftBody.length)
                                    draftLoading = false
                                    refs.syncDot.setTextColor(0xFF34A853.toInt())
                                    refs.subtitleText.text = if (draftBody.isNotEmpty()) "typing..." else conv.address
                                }
                            }
                        }
                    }
                }
            }
        }, 1500, 1500)
    }

    private fun rebuildComposeBar(ctx: Context, address: String, refs: ChatRefs, oldBodyEdit: EditText?) {
        val (compBar, newBodyEdit) = buildComposeBar(ctx, address, false) { recipient, body ->
            sendSms(ctx, recipient, body) {
                loadMessages(address, refs.msgContainer)
            }
        }
        currentBodyEdit = newBodyEdit
        refs.composeBar.removeAllViews()
        refs.composeBar.addView(compBar)

        // Copy text and cursor from old body edit
        oldBodyEdit?.let { old ->
            val txt = old.text?.toString() ?: ""
            if (txt.isNotEmpty()) {
                newBodyEdit?.setText(txt)
                newBodyEdit?.setSelection(txt.length)
            }
        }

        // Reattach text watcher on new body edit
        val watcher = object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) {
                if (draftLoading) return
                draftDirty = true
                draftTimer?.cancel(); draftTimer = java.util.Timer(true)
                draftTimer?.schedule(object : java.util.TimerTask() {
                    override fun run() {
                        val c = ctxRef ?: return
                        val conv = currentConv ?: return
                        saveDraftToVpc(c, conv.address, s?.toString() ?: "") { draftDirty = false }
                    }
                }, 300)
            }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        }
        newBodyEdit?.tag = watcher
        newBodyEdit?.addTextChangedListener(watcher)
    }

    private class ChatRefs(
        val titleText: TextView, val subtitleText: TextView, val msgContainer: LinearLayout,
        val scroll: ScrollView, val composeBar: LinearLayout, val backBtn: TextView, val deleteBtn: TextView,
        val syncDot: TextView
    )

    private fun buildChatDetailLayout(ctx: Context): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val layout = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }

        val header = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL; setPadding(dp(6), dp(8), dp(6), dp(8))
            setBackgroundColor(0xFF181818.toInt()); gravity = Gravity.CENTER_VERTICAL
        }
        val backBtn = buildIconBtn(ctx, "\u2190", dp(42))
        val avatar = makeAvatar(ctx, "", 0xFF555555.toInt(), dp(36))
        val titleBlock = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            setPadding(dp(10), 0, dp(8), 0)
        }
        val titleText = TextView(ctx).apply {
            setTextColor(0xFFDDDDDD.toInt()); textSize = 15f; maxLines = 1
            setTypeface(null, android.graphics.Typeface.BOLD)
        }
        val subtitleText = TextView(ctx).apply {
            setTextColor(0xFF888888.toInt()); textSize = 11f; maxLines = 1
        }
        titleBlock.addView(titleText); titleBlock.addView(subtitleText)
        val syncDot = TextView(ctx).apply {
            text = "\u25CF"; setTextColor(0xFF444444.toInt()); textSize = 10f
            gravity = Gravity.CENTER; width = dp(24); height = dp(24)
        }
        val deleteBtn = buildIconBtn(ctx, "\uD83D\uDDD1", dp(42))
        header.addView(backBtn); header.addView(avatar); header.addView(titleBlock); header.addView(syncDot); header.addView(deleteBtn)

        val chatScroll = ScrollView(ctx).apply {
            setBackgroundColor(0xFF111111.toInt())
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
        }
        val msgContainer = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL; setPadding(0, dp(8), 0, dp(8))
        }
        chatScroll.addView(msgContainer)
        val composeBar = LinearLayout(ctx)

        layout.addView(header); layout.addView(chatScroll); layout.addView(composeBar)
        layout.tag = ChatRefs(titleText, subtitleText, msgContainer, chatScroll, composeBar, backBtn, deleteBtn, syncDot)
        return layout
    }

    private fun buildConversationListLayout(ctx: Context): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val layout = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }

        val header = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL; setPadding(dp(14), dp(12), dp(14), dp(10))
            setBackgroundColor(0xFF181818.toInt()); gravity = Gravity.CENTER_VERTICAL
        }
        header.addView(TextView(ctx).apply {
            text = "Messages"; setTextColor(0xFFDDDDDD.toInt()); textSize = 17f
            setTypeface(null, android.graphics.Typeface.BOLD)
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
        })
        header.addView(TextView(ctx).apply {
            text = "+ New"; setTextColor(0xFFAAAAAA.toInt()); textSize = 14f
            setPadding(dp(8), dp(4), dp(8), dp(4))
            setOnClickListener {
                val input = EditText(ctx).apply {
                    hint = "Phone number"; setHintTextColor(0xFF555555.toInt())
                    setTextColor(0xFFDDDDDD.toInt()); setBackgroundColor(0xFF1E1E1E.toInt())
                    setPadding(dp(16), dp(14), dp(16), dp(14)); textSize = 14f
                    inputType = android.text.InputType.TYPE_CLASS_PHONE
                }
                android.app.AlertDialog.Builder(ctx).setTitle("New message")
                    .setView(input)
                    .setPositiveButton("OK") { _, _ ->
                        val t = input.text.toString().trim()
                        if (t.isNotBlank()) showChat(Conversation(t, t, 0, "", 0, 1))
                    }.setNegativeButton("Cancel", null).show()
            }
        })
        layout.addView(header)

        val cs = ScrollView(ctx)
        convListScroll = cs
        val cc = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }
        convListContainer = cc
        cs.addView(cc)
        layout.addView(cs, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f))
        return layout
    }

    private fun loadConversations(c: LinearLayout) {
        val ctx = ctxRef ?: return; c.removeAllViews()
        c.addView(centerLabel(ctx, "Loading..."))
        thread(name = "gafam-load-convs", isDaemon = true) {
            val convs = buildConversations(ctx)
            (ctx as? android.app.Activity)?.runOnUiThread { renderConversations(ctx, convs, c) }
        }
    }

    private fun buildConversations(ctx: Context): List<Conversation> {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED) return emptyList()
        val map = mutableMapOf<String, MutableList<SmsMsg>>()
        var totalMsgs = 0
        ctx.contentResolver.query(Uri.parse("content://sms"),
            arrayOf("_id","address","body","date","type"),
            null, null, "date DESC")?.use {
            val idI = it.getColumnIndex("_id"); val aI = it.getColumnIndex("address")
            val bI = it.getColumnIndex("body"); val dI = it.getColumnIndex("date")
            val tI = it.getColumnIndex("type")
            while (it.moveToNext() && totalMsgs < 500) {
                totalMsgs++
                val a = it.getString(aI) ?: ""; if (a.isBlank()) continue
                val msg = SmsMsg(it.getLong(idI), a, it.getString(bI) ?: "", it.getLong(dI), it.getInt(tI))
                val norm = a.filter { c -> c.isDigit() }.takeLast(9)
                if (norm.length >= 4) map.getOrPut(norm) { mutableListOf() }.add(msg)
                else if (a.isNotBlank()) map.getOrPut(a) { mutableListOf() }.add(msg)
            }
        }
        return map.map { (_, msgs) ->
            val last = msgs.first()
            Conversation(last.address, lookupName(ctx, last.address), last.date, last.body, msgs.size, last.type)
        }.sortedByDescending { it.lastDate }
    }

    private fun renderConversations(ctx: Context, convs: List<Conversation>, c: LinearLayout) {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        c.removeAllViews()
        val div = 0xFF1E1E1E.toInt()
        if (convs.isEmpty()) {
            val emptyLayout = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER
                setPadding(0, dp(80), 0, 0)
            }
            emptyLayout.addView(TextView(ctx).apply {
                text = "\u2709"; setTextColor(0xFF444444.toInt()); textSize = 36f
                gravity = Gravity.CENTER
            })
            emptyLayout.addView(TextView(ctx).apply {
                text = "No conversations"; setTextColor(0xFF666666.toInt()); textSize = 14f
                gravity = Gravity.CENTER; setPadding(0, dp(12), 0, 0)
            })
            c.addView(emptyLayout); return
        }
        for (conv in convs) {
            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.HORIZONTAL; setPadding(dp(14), dp(12), dp(10), dp(12))
                gravity = Gravity.CENTER_VERTICAL
                setOnClickListener { showChat(conv) }
                setOnLongClickListener {
                    android.app.AlertDialog.Builder(ctx)
                        .setTitle("Delete?").setMessage("Delete all messages with ${conv.name}?")
                        .setPositiveButton("Delete") { _, _ -> deleteConversation(ctx, conv.address); loadConversations(c) }
                        .setNegativeButton("Cancel", null).show(); true
                }
            }
            row.addView(makeAvatar(ctx, conv.name.take(1).uppercase(), 0xFF555555.toInt(), dp(42)))
            val col = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
                setPadding(dp(12), 0, dp(8), 0)
            }
            col.addView(TextView(ctx).apply {
                text = conv.name; setTextColor(0xFFDDDDDD.toInt()); textSize = 15f; maxLines = 1
            })
            col.addView(TextView(ctx).apply {
                text = conv.lastBody.take(70)
                setTextColor(0xFF888888.toInt()); textSize = 13f; maxLines = 1
                setPadding(0, dp(3), 0, 0)
            })
            val meta = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL; gravity = Gravity.END
                setPadding(0, 0, dp(4), 0)
            }
            meta.addView(TextView(ctx).apply {
                text = formatConvTime(conv.lastDate); setTextColor(0xFF666666.toInt()); textSize = 11f
            })
            if (conv.count > 1) meta.addView(TextView(ctx).apply {
                text = "${conv.count}"; setTextColor(0xFF111111.toInt())
                setBackgroundColor(0xFF666666.toInt()); textSize = 10f; gravity = Gravity.CENTER
                width = dp(20); height = dp(20)
                background = android.graphics.drawable.GradientDrawable().apply {
                    setColor(0xFF666666.toInt()); shape = android.graphics.drawable.GradientDrawable.OVAL
                }
                setPadding(0, dp(2), 0, 0)
            })
            row.addView(col); row.addView(meta); c.addView(row)
            c.addView(View(ctx).apply {
                layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1)
                setBackgroundColor(div)
            })
        }
    }

    private fun loadMessages(address: String, container: LinearLayout) {
        val ctx = ctxRef ?: return; container.removeAllViews()
        container.addView(centerLabel(ctx, "Loading..."))
        thread(name = "gafam-load-msgs", isDaemon = true) {
            val msgs = readMessages(ctx, address)
            (ctx as? android.app.Activity)?.runOnUiThread {
                renderMessages(ctx, msgs, container)
                container.post { (container.parent as? ScrollView)?.fullScroll(View.FOCUS_DOWN) }
            }
        }
    }

    private fun readMessages(ctx: Context, address: String): List<SmsMsg> {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED) return emptyList()
        val list = mutableListOf<SmsMsg>()
        ctx.contentResolver.query(Uri.parse("content://sms"),
            arrayOf("_id","address","body","date","type"),
            null, null, "date ASC")?.use {
            val idI = it.getColumnIndex("_id"); val aI = it.getColumnIndex("address")
            val bI = it.getColumnIndex("body"); val dI = it.getColumnIndex("date"); val tI = it.getColumnIndex("type")
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
        val da = d(a); val db = d(b)
        if (da.length >= 4 && db.length >= 4 && da == db) return true
        return a.equals(b, ignoreCase = true)
    }

    private fun renderMessages(ctx: Context, msgs: List<SmsMsg>, container: LinearLayout) {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        container.removeAllViews()
        if (msgs.isEmpty()) {
            val emptyLayout = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER
                setPadding(0, dp(80), 0, 0)
            }
            emptyLayout.addView(TextView(ctx).apply {
                text = "\uD83D\uDCAC"; setTextColor(0xFF444444.toInt()); textSize = 36f
                gravity = Gravity.CENTER
            })
            emptyLayout.addView(TextView(ctx).apply {
                text = "No messages yet"; setTextColor(0xFF666666.toInt()); textSize = 14f
                gravity = Gravity.CENTER; setPadding(0, dp(12), 0, 0)
            })
            container.addView(emptyLayout); return
        }

        val mw = ctx.resources.displayMetrics.widthPixels - dp(64)

        var lastDateBlock: String? = null
        var lastMsgType: Int? = null
        for (msg in msgs) {
            val db = formatDateBlock(msg.date)
            if (db != lastDateBlock) {
                lastDateBlock = db
                lastMsgType = null
                container.addView(TextView(ctx).apply {
                    text = db; setTextColor(0xFF777777.toInt()); textSize = 12f
                    gravity = Gravity.CENTER; setPadding(0, dp(20), 0, dp(12))
                })
            }
            val isOut = msg.type == 2
            val firstInGroup = lastMsgType != msg.type
            lastMsgType = msg.type

            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(8), if (firstInGroup) dp(6) else dp(1), dp(8), dp(1))
                gravity = if (isOut) Gravity.END else Gravity.START
            }

            if (firstInGroup) {
                row.addView(TextView(ctx).apply {
                    text = if (isOut) "You" else (currentConv?.name ?: msg.address)
                    setTextColor(0xFF888888.toInt()); textSize = 11f
                    setPadding(dp(4), 0, dp(4), dp(4))
                })
            }

            val bubbleBg = android.graphics.drawable.GradientDrawable().apply {
                setColor(if (isOut) 0xFF1E4D2B.toInt() else 0xFF2A2A2A.toInt())
                cornerRadius = dp(16).toFloat()
            }
            val bubble = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(14), dp(12), dp(14), dp(12))
                background = bubbleBg
                setOnLongClickListener {
                    val clip = android.content.ClipData.newPlainText("sms", msg.body)
                    (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                        .setPrimaryClip(clip)
                    android.widget.Toast.makeText(ctx, "Copied", android.widget.Toast.LENGTH_SHORT).show()
                    true
                }
            }
            bubble.addView(TextView(ctx).apply {
                text = msg.body; setTextColor(0xFFDDDDDD.toInt()); textSize = 14f
                setLineSpacing(dp(3).toFloat(), 1.4f)
                maxWidth = mw
            })
            bubble.addView(TextView(ctx).apply {
                text = formatTime(msg.date)
                setTextColor(if (isOut) 0xFFAACCDD.toInt() else 0xFF777777.toInt())
                textSize = 10f; gravity = Gravity.END
                setPadding(0, dp(4), 0, 0)
            })
            row.addView(bubble)
            container.addView(row)
        }
    }

    private fun buildComposeBar(
        ctx: Context, recipient: String, isNew: Boolean,
        onSend: (recipient: String, body: String) -> Unit
    ): Pair<LinearLayout, EditText?> {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val bar = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(0xFF111111.toInt())
            setPadding(dp(4), 0, dp(4), dp(6))
        }

        var recipientInput: EditText? = null
        if (isNew) {
            recipientInput = EditText(ctx).apply {
                hint = "To"; setHintTextColor(0xFF777777.toInt())
                setTextColor(0xFFDDDDDD.toInt()); setBackgroundColor(0xFF2C2C2E.toInt())
                setPadding(dp(16), dp(12), dp(16), dp(12)); textSize = 14f; maxLines = 1
                inputType = android.text.InputType.TYPE_CLASS_PHONE; setSingleLine(true)
                setText(recipient)
                background = android.graphics.drawable.GradientDrawable().apply {
                    setColor(0xFF2C2C2E.toInt()); cornerRadius = dp(22).toFloat()
                }
                layoutParams = LinearLayout.LayoutParams(
                    LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT
                ).apply { setMargins(dp(8), 0, 0, dp(4)) }
            }
            bar.addView(recipientInput)
        }

        // QKSMS-inspired bubble: rounded rectangle containing input + send
        val bubble = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(4), dp(4), dp(8), dp(4))
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(0xFF2C2C2E.toInt())
                cornerRadius = dp(22).toFloat()
            }
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(dp(8), dp(2), 0, 0) }
        }

        val bodyInput = EditText(ctx).apply {
            hint = "Message"; setHintTextColor(0xFF777777.toInt())
            setTextColor(0xFFDDDDDD.toInt()); setBackgroundColor(android.graphics.Color.TRANSPARENT)
            setPadding(dp(12), dp(10), dp(8), dp(10)); textSize = 15f
            maxLines = 5; minLines = 1; minHeight = dp(44)
            gravity = Gravity.CENTER_VERTICAL or Gravity.START
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            setSingleLine(false)
        }

        val sendBtn = TextView(ctx).apply {
            text = "\u2192"; setTextColor(0xFF555555.toInt()); textSize = 20f
            gravity = Gravity.CENTER; width = dp(40); height = dp(40)
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(0xFF444444.toInt()); shape = android.graphics.drawable.GradientDrawable.OVAL
            }
            layoutParams = LinearLayout.LayoutParams(dp(40), dp(40))
            isEnabled = false
        }

        // Enable/disable with alpha (QKSMS style)
        fun setSendEnabled(enabled: Boolean) {
            sendBtn.isEnabled = enabled
            sendBtn.alpha = if (enabled) 1f else 0.4f
            sendBtn.setTextColor(if (enabled) 0xFF111111.toInt() else 0xFF888888.toInt())
            (sendBtn.background as? android.graphics.drawable.GradientDrawable)?.setColor(
                if (enabled) 0xFFAAAAAA.toInt() else 0xFF444444.toInt())
        }

        sendBtn.setOnClickListener {
            val b = bodyInput.text.toString().trim()
            val r = if (isNew) recipientInput?.text?.toString()?.trim() ?: recipient else recipient
            if (r.isNotEmpty() && b.isNotEmpty()) {
                sendBtn.animate().scaleX(0.85f).scaleY(0.85f).setDuration(80).withEndAction {
                    sendBtn.animate().scaleX(1f).scaleY(1f).setDuration(80)
                }
                onSend(r, b); bodyInput.text.clear()
            }
        }

        // Character counter below bubble
        val charCount = TextView(ctx).apply {
            text = ""; setTextColor(0xFF555555.toInt()); textSize = 10f
            gravity = Gravity.END; setPadding(0, dp(2), dp(14), 0)
        }

        // Single TextWatcher for enable state + char count
        val watcher = object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) {
                val len = s?.length ?: 0; charCount.text = if (len > 0) "$len/160" else ""
                val hasBody = s?.isNotEmpty() == true
                val hasRec = if (isNew) recipientInput?.text?.toString()?.trim()?.isNotEmpty() == true else true
                setSendEnabled(hasBody && hasRec)
            }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        }
        bodyInput.addTextChangedListener(watcher)
        if (isNew) recipientInput?.addTextChangedListener(watcher)

        bubble.addView(bodyInput); bubble.addView(sendBtn)
        bar.addView(bubble); bar.addView(charCount)
        return bar to bodyInput
    }

    private fun sendSms(ctx: Context, recipient: String, body: String, reload: () -> Unit) {
        try {
            val mgr = SmsManager.getDefault()
            val parts = mgr.divideMessage(body)
            val beforeMsgs = readMessages(ctx, recipient)
            val beforeCount = beforeMsgs.size
            if (parts.size > 1) mgr.sendMultipartTextMessage(recipient, null, parts, null, null)
            else mgr.sendTextMessage(recipient, null, body, null, null)
            android.widget.Toast.makeText(ctx, "Sent", android.widget.Toast.LENGTH_SHORT).show()
            thread(name = "gafam-sms-reload", isDaemon = true) {
                for (attempt in 0 until 8) {
                    Thread.sleep(400)
                    val msgs = readMessages(ctx, recipient)
                    if (msgs.size > beforeCount) {
                        (ctx as? android.app.Activity)?.runOnUiThread { reload() }
                        return@thread
                    }
                }
                // Fallback: reload anyway after timeout
                (ctx as? android.app.Activity)?.runOnUiThread { reload() }
            }
            SmsHistorySync.syncAsync(ctx, force = true)
        } catch (e: Exception) {
            android.widget.Toast.makeText(ctx, "Error: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    private fun deleteConversation(ctx: Context, address: String) {
        if (ContextCompat.checkSelfPermission(ctx, "android.permission.WRITE_SMS")
            != PackageManager.PERMISSION_GRANTED) {
            (ctx as? android.app.Activity)?.runOnUiThread {
                android.widget.Toast.makeText(ctx, "Need WRITE_SMS permission", android.widget.Toast.LENGTH_LONG).show()
            }; return
        }
        thread(name = "gafam-delete-conv", isDaemon = true) {
            try {
                val norm = address.filter { it.isDigit() }.takeLast(9)
                val addrs = mutableSetOf(address)
                ctx.contentResolver.query(Uri.parse("content://sms"),
                    arrayOf("DISTINCT address"), null, null, null)?.use { c ->
                    val ai = c.getColumnIndex("address")
                    while (c.moveToNext()) {
                        val a = c.getString(ai) ?: ""
                        if (a.filter { it.isDigit() }.takeLast(9) == norm || a == address) addrs.add(a)
                    }
                }
                var total = 0
                for (a in addrs) {
                    total += ctx.contentResolver.delete(Uri.parse("content://sms"), "address = ?", arrayOf(a))
                }
                val msg = "Deleted $total messages"
                (ctx as? android.app.Activity)?.runOnUiThread {
                    android.widget.Toast.makeText(ctx, msg, android.widget.Toast.LENGTH_SHORT).show()
                }
            } catch (e: Exception) {
                (ctx as? android.app.Activity)?.runOnUiThread {
                    android.widget.Toast.makeText(ctx, "Delete failed: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    private fun saveDraftToVpc(ctx: Context, peer: String, body: String, onDone: (() -> Unit)? = null) {
        val prefs = ctx.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl == null || jwtSecret == null) { onDone?.invoke(); return }
        val myPhone = prefs.getString("myPhoneNumber", "") ?: ""
        thread(name = "gafam-draft-save", isDaemon = true) {
            try {
                val plaintext = JSONObject().apply { put("phone", myPhone); put("peer", peer); put("body", body) }
                    .toString().toByteArray(Charsets.UTF_8)
                val digest = java.security.MessageDigest.getInstance("SHA-256")
                val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
                val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")
                val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
                val iv = ByteArray(12); java.security.SecureRandom().nextBytes(iv)
                cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, secretKey, javax.crypto.spec.GCMParameterSpec(128, iv))
                val ct = cipher.doFinal(plaintext)
                val payload = JSONObject().apply {
                    put("encrypted_data", android.util.Base64.encodeToString(ct, android.util.Base64.NO_WRAP))
                    put("iv", android.util.Base64.encodeToString(iv, android.util.Base64.NO_WRAP))
                }
                val client = ApiClient.getClient(ctx) ?: run { onDone?.invoke(); return@thread }
                val req = Request.Builder().url(ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/draft"))
                    .put(payload.toString().toRequestBody("application/json".toMediaType()))
                    .addHeader("Authorization", "Bearer $jwtSecret").build()
                val resp = client.newCall(req).execute()
                val respStr = resp.body?.string() ?: ""
                resp.close()
                val respJson = JSONObject(respStr)
                val updatedAt = respJson.optString("updated_at", "")
                if (updatedAt.isNotEmpty()) lastDraftVersion = updatedAt
                onDone?.invoke()
            } catch (_: Exception) { onDone?.invoke() }
        }
    }

    private fun loadDraftFromVpc(ctx: Context, peer: String, cb: (body: String, updatedAt: String) -> Unit) {
        val prefs = ctx.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        val myPhone = prefs.getString("myPhoneNumber", "") ?: ""
        thread(name = "gafam-draft-load", isDaemon = true) {
            try {
                val client = ApiClient.getClient(ctx) ?: return@thread
                val req = Request.Builder()
                    .url(ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/draft?peer=${Uri.encode(peer)}&phone=${Uri.encode(myPhone)}"))
                    .addHeader("Authorization", "Bearer $jwtSecret").build()
                val resp = client.newCall(req).execute()
                val body = resp.body?.string() ?: ""; resp.close()
                val json = JSONObject(body)
                cb(json.optString("body", ""), json.optString("updated_at", ""))
            } catch (_: Exception) { cb("", "") }
        }
    }

    private fun lookupName(ctx: Context, phone: String): String {
        val norm = phone.filter { it.isDigit() }.takeLast(9)
        if (norm.length < 7) {
            contactCache[phone] = phone
            return phone
        }
        contactCache[norm]?.let { return it }
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_CONTACTS)
            == PackageManager.PERMISSION_GRANTED) {
            try {
                ctx.contentResolver.query(
                    android.net.Uri.withAppendedPath(
                        ContactsContract.PhoneLookup.CONTENT_FILTER_URI, android.net.Uri.encode(phone)),
                    arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME), null, null, null)?.use {
                    if (it.moveToFirst()) {
                        val n = it.getString(it.getColumnIndex(ContactsContract.PhoneLookup.DISPLAY_NAME))
                        if (n != null) { contactCache[norm] = n; return n }
                    }
                }
            } catch (_: Exception) {}
        }
        contactCache[norm] = phone; return phone
    }

    private fun makeAvatar(ctx: Context, letter: String, bg: Int, sizePx: Int): TextView {
        return TextView(ctx).apply {
            text = letter; setTextColor(0xFF111111.toInt()); textSize = 14f
            gravity = Gravity.CENTER; width = sizePx; height = sizePx
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(bg); shape = android.graphics.drawable.GradientDrawable.OVAL
            }
        }
    }

    private fun buildIconBtn(ctx: Context, text: String, size: Int): TextView {
        return TextView(ctx).apply {
            this.text = text; setTextColor(0xFFCCCCCC.toInt()); textSize = 18f
            gravity = Gravity.CENTER; width = size; height = size
            setOnClickListener {}
        }
    }

    private fun centerLabel(ctx: Context, msg: String): TextView {
        return TextView(ctx).apply {
            text = msg; setTextColor(0xFF666666.toInt()); textSize = 14f
            gravity = Gravity.CENTER; setPadding(0, 64, 0, 0)
        }
    }

    private fun formatConvTime(ts: Long): String {
        if (ts == 0L) return ""
        val diff = System.currentTimeMillis() - ts
        return when {
            diff < 60_000L -> "now"
            diff < 3600_000L -> "${diff / 60_000L}m"
            diff < 86_400_000L -> "${diff / 3600_000L}h"
            diff < 172_800_000L -> "yesterday"
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
        val sameDay = c.get(java.util.Calendar.YEAR) == now.get(java.util.Calendar.YEAR) &&
            c.get(java.util.Calendar.DAY_OF_YEAR) == now.get(java.util.Calendar.DAY_OF_YEAR)
        if (sameDay) return "Today"
        val days = arrayOf("Sun","Mon","Tue","Wed","Thu","Fri","Sat")
        return "${days[c.get(java.util.Calendar.DAY_OF_WEEK) - 1]} " +
            "${"%02d".format(c.get(java.util.Calendar.DAY_OF_MONTH))}/" +
            "${"%02d".format(c.get(java.util.Calendar.MONTH) + 1)}/${c.get(java.util.Calendar.YEAR)}"
    }

    private fun formatTime(ts: Long): String {
        val c = java.util.Calendar.getInstance().apply { timeInMillis = ts }
        return "${"%02d".format(c.get(java.util.Calendar.HOUR_OF_DAY))}:" +
            "${"%02d".format(c.get(java.util.Calendar.MINUTE))}"
    }
}
