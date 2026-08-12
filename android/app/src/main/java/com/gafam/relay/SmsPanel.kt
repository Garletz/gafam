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
    private var chatDetailLayout: LinearLayout? = null
    private var mainContainer: LinearLayout? = null
    private var ctxRef: Context? = null
    private var currentConv: Conversation? = null
    private var draftTimer: java.util.Timer? = null
    private var draftPollTimer: java.util.Timer? = null
    @Volatile private var draftDirty = false
    @Volatile private var draftLoading = false
    private var lastDraftVersion: String? = null

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
        return root
    }

    fun onDestroy() { draftTimer?.cancel(); draftPollTimer?.cancel() }

    // ── Navigation ──

    private fun showConversations() {
        currentConv = null; draftPollTimer?.cancel()
        val mc = mainContainer ?: return; mc.removeAllViews(); mc.addView(convListLayout)
        val cs = convListLayout?.getChildAt(1) as? ScrollView
        (cs?.getChildAt(0) as? LinearLayout)?.let { loadConversations(it) }
    }

    private fun showChat(conv: Conversation) {
        val ctx = ctxRef ?: return; val mc = mainContainer ?: return
        val cl = chatDetailLayout ?: return; val refs = cl.tag as? ChatRefs ?: return
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        currentConv = conv

        refs.titleText.text = conv.name
        refs.subtitleText.text = conv.address
        refs.backBtn.setOnClickListener { showConversations() }

        // Delete
        refs.deleteBtn.setOnClickListener {
            android.app.AlertDialog.Builder(ctx)
                .setTitle("Delete conversation").setMessage("Delete all messages with ${conv.name}?")
                .setPositiveButton("Delete") { _, _ -> deleteConversation(ctx, conv.address); showConversations() }
                .setNegativeButton("Cancel", null).show()
        }

        // Compose bar — no recipient field for known conversations
        refs.composeBar.removeAllViews()
        val isNew = conv.address == conv.name && conv.count == 0
        val (compBar, bodyEdit) = buildComposeBar(ctx, conv.address, isNew) { recipient, body ->
            sendSms(ctx, recipient, body) { loadMessages(conv.address, refs.msgContainer) }
        }
        refs.composeBar.addView(compBar)

        // Draft sync
        lastDraftVersion = null // reset on new conversation
        loadDraftFromVpc(ctx, conv.address) { draftBody, updatedAt ->
            if (currentConv?.address == conv.address) {
                lastDraftVersion = updatedAt
                draftLoading = true
                bodyEdit?.setText(draftBody)
                if (draftBody.isNotEmpty()) bodyEdit?.setSelection(draftBody.length)
                draftLoading = false
                (ctx as? android.app.Activity)?.runOnUiThread { refs.syncDot.setTextColor(0xFF34A853.toInt()) }
            }
        }

        // Clean old text watchers
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
                        saveDraftToVpc(ctx, conv.address, s?.toString() ?: "")
                        draftDirty = false
                    }
                }, 300)
            }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        }
        bodyEdit?.tag = watcher
        bodyEdit?.addTextChangedListener(watcher)

        draftPollTimer?.cancel(); draftPollTimer = java.util.Timer(true)
        draftPollTimer?.schedule(object : java.util.TimerTask() {
            override fun run() {
                if (currentConv?.address != conv.address) return
                if (draftDirty) return  // user is typing locally — don't overwrite
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
                                }
                            }
                        }
                    }
                }
            }
        }, 1500, 1500)

        loadMessages(conv.address, refs.msgContainer)
        mc.removeAllViews(); mc.addView(cl)
        refs.scroll.post { refs.scroll.fullScroll(View.FOCUS_DOWN) }
    }

    // ── Chat detail layout (header + messages + compose) ──

    private class ChatRefs(
        val titleText: TextView, val subtitleText: TextView, val msgContainer: LinearLayout,
        val scroll: ScrollView, val composeBar: LinearLayout, val backBtn: TextView, val deleteBtn: TextView,
        val syncDot: TextView
    )

    private fun buildChatDetailLayout(ctx: Context): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val layout = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }

        // ── Header ──
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

    // ── Conversation list ──

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
        val cc = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }
        cs.addView(cc)
        layout.addView(cs, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f))
        return layout
    }

    // ── Load & render conversations ──

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
        ctx.contentResolver.query(Uri.parse("content://sms"),
            arrayOf("_id","address","body","date","type"),
            null, null, "date DESC")?.use {
            val idI = it.getColumnIndex("_id"); val aI = it.getColumnIndex("address")
            val bI = it.getColumnIndex("body"); val dI = it.getColumnIndex("date")
            val tI = it.getColumnIndex("type")
            while (it.moveToNext()) {
                val a = it.getString(aI) ?: ""; if (a.isBlank()) continue
                val msg = SmsMsg(it.getLong(idI), a, it.getString(bI) ?: "", it.getLong(dI), it.getInt(tI))
                val norm = a.filter { c -> c.isDigit() }.takeLast(9)
                if (norm.length >= 7) map.getOrPut(norm) { mutableListOf() }.add(msg)
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
        if (convs.isEmpty()) { c.addView(centerLabel(ctx, "No conversations yet")); return }
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
            val prefix = if (conv.lastType == 2) "You: " else ""
            col.addView(TextView(ctx).apply {
                text = conv.name; setTextColor(0xFFDDDDDD.toInt()); textSize = 15f; maxLines = 1
            })
            col.addView(TextView(ctx).apply {
                text = (prefix + conv.lastBody).take(70)
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

    // ── Load & render messages ──

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
        return d(a).length >= 7 && d(b).length >= 7 && d(a) == d(b)
    }

    private fun renderMessages(ctx: Context, msgs: List<SmsMsg>, container: LinearLayout) {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        container.removeAllViews()
        if (msgs.isEmpty()) { container.addView(centerLabel(ctx, "No messages yet")); return }

        val bubbleIn = android.graphics.drawable.GradientDrawable().apply {
            setColor(0xFF2A2A2A.toInt()); cornerRadius = dp(14).toFloat()
        }
        val bubbleOut = android.graphics.drawable.GradientDrawable().apply {
            setColor(0xFF1A3A5A.toInt()); cornerRadius = dp(14).toFloat()
        }
        val mw = ctx.resources.displayMetrics.widthPixels - dp(64)

        var lastDateBlock: String? = null
        for (msg in msgs) {
            val db = formatDateBlock(msg.date)
            if (db != lastDateBlock) {
                lastDateBlock = db
                container.addView(TextView(ctx).apply {
                    text = db; setTextColor(0xFF777777.toInt()); textSize = 12f
                    gravity = Gravity.CENTER; setPadding(0, dp(20), 0, dp(12))
                })
            }
            val isOut = msg.type == 2
            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(8), dp(3), dp(8), dp(3))
                gravity = if (isOut) Gravity.END else Gravity.START
            }
            val bubble = TextView(ctx).apply {
                text = msg.body; setTextColor(0xFFDDDDDD.toInt()); textSize = 14f
                setPadding(dp(14), dp(10), dp(14), dp(10))
                background = if (isOut) bubbleOut else bubbleIn; maxWidth = mw
                setLineSpacing(dp(2).toFloat(), 1f)
                setOnLongClickListener {
                    val clip = android.content.ClipData.newPlainText("sms", msg.body)
                    (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                        .setPrimaryClip(clip)
                    android.widget.Toast.makeText(ctx, "Copied", android.widget.Toast.LENGTH_SHORT).show(); true
                }
            }
            val timeLbl = TextView(ctx).apply {
                text = formatTime(msg.date); setTextColor(0xFF555555.toInt()); textSize = 11f
                gravity = if (isOut) Gravity.END else Gravity.START
                setPadding(dp(8), dp(4), dp(8), 0)
            }
            row.addView(bubble); row.addView(timeLbl); container.addView(row)
        }
    }

    // ── Compose bar (full width, no recipient for known conversations) ──

    private fun buildComposeBar(
        ctx: Context, recipient: String, isNew: Boolean,
        onSend: (recipient: String, body: String) -> Unit
    ): Pair<LinearLayout, EditText?> {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val bar = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL; setBackgroundColor(0xFF181818.toInt())
            setPadding(dp(4), dp(4), dp(4), dp(8))
        }

        var recipientInput: EditText? = null
        if (isNew) {
            recipientInput = EditText(ctx).apply {
                hint = "Recipient"; setHintTextColor(0xFF555555.toInt())
                setTextColor(0xFFDDDDDD.toInt()); setBackgroundColor(0xFF1E1E1E.toInt())
                setPadding(dp(12), dp(8), dp(12), dp(8)); textSize = 13f; maxLines = 1
                inputType = android.text.InputType.TYPE_CLASS_PHONE; setSingleLine(true)
                setText(recipient)
                layoutParams = LinearLayout.LayoutParams(
                    LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT
                ).apply { setMargins(dp(4), 0, dp(4), dp(4)) }
            }
            bar.addView(recipientInput)
        }

        val inputRow = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER
            setPadding(dp(4), 0, dp(4), 0)
        }
        val bodyInput = EditText(ctx).apply {
            hint = "Message..."; setHintTextColor(0xFF555555.toInt())
            setTextColor(0xFFDDDDDD.toInt()); setBackgroundColor(0xFF222222.toInt())
            setPadding(dp(14), dp(12), dp(14), dp(12)); textSize = 15f
            maxLines = 8; minLines = 1; gravity = Gravity.TOP or Gravity.START
            layoutParams = LinearLayout.LayoutParams(0, dp(48), 1f)
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(0xFF222222.toInt()); cornerRadius = dp(24).toFloat()
            }
            setSingleLine(false)
        }
        val sendBtn = TextView(ctx).apply {
            text = "\u25B6"; setTextColor(0xFF111111.toInt()); textSize = 16f
            gravity = Gravity.CENTER; width = dp(44); height = dp(44)
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(0xFFAAAAAA.toInt()); shape = android.graphics.drawable.GradientDrawable.OVAL
            }
            layoutParams = LinearLayout.LayoutParams(dp(44), dp(44)).apply { setMargins(dp(8), 0, 0, 0) }
            setOnClickListener {
                val b = bodyInput.text.toString().trim()
                val r = if (isNew) recipientInput?.text?.toString()?.trim() ?: recipient else recipient
                if (r.isNotEmpty() && b.isNotEmpty()) { onSend(r, b); bodyInput.text.clear() }
            }
        }
        inputRow.addView(bodyInput); inputRow.addView(sendBtn)
        bar.addView(inputRow)

        return bar to bodyInput
    }

    // ── Send ──

    private fun sendSms(ctx: Context, recipient: String, body: String, reload: () -> Unit) {
        try {
            val mgr = SmsManager.getDefault()
            val parts = mgr.divideMessage(body)
            if (parts.size > 1) mgr.sendMultipartTextMessage(recipient, null, parts, null, null)
            else mgr.sendTextMessage(recipient, null, body, null, null)
            android.widget.Toast.makeText(ctx, "Sent", android.widget.Toast.LENGTH_SHORT).show()
            thread(name = "gafam-sms-reload", isDaemon = true) {
                Thread.sleep(800)
                (ctx as? android.app.Activity)?.runOnUiThread { reload() }
            }
            SmsHistorySync.syncAsync(ctx, force = true)
        } catch (e: Exception) {
            android.widget.Toast.makeText(ctx, "Error: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    // ── Delete ──

    private fun deleteConversation(ctx: Context, address: String) {
        if (ContextCompat.checkSelfPermission(ctx, "android.permission.WRITE_SMS")
            != PackageManager.PERMISSION_GRANTED) {
            android.widget.Toast.makeText(ctx, "Need WRITE_SMS permission", android.widget.Toast.LENGTH_LONG).show(); return
        }
        try {
            val n = ctx.contentResolver.delete(Uri.parse("content://sms"), "address = ?", arrayOf(address))
            android.widget.Toast.makeText(ctx, "Deleted $n messages", android.widget.Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            android.widget.Toast.makeText(ctx, "Delete failed: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    // ── Draft sync ──

    private fun saveDraftToVpc(ctx: Context, peer: String, body: String) {
        val prefs = ctx.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
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
                val client = ApiClient.getClient(ctx) ?: return@thread
                val req = Request.Builder().url(ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/draft"))
                    .put(payload.toString().toRequestBody("application/json".toMediaType()))
                    .addHeader("Authorization", "Bearer $jwtSecret").build()
                val resp = client.newCall(req).execute()
                val respStr = resp.body?.string() ?: ""
                resp.close()
                val respJson = JSONObject(respStr)
                val updatedAt = respJson.optString("updated_at", "")
                if (updatedAt.isNotEmpty()) lastDraftVersion = updatedAt
            } catch (_: Exception) {}
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

    // ── Contact name ──

    private fun lookupName(ctx: Context, phone: String): String {
        val norm = phone.filter { it.isDigit() }.takeLast(9)
        if (norm.length < 7) return phone
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

    // ── UI helpers ──

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
            setOnClickListener {} // set by caller
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
