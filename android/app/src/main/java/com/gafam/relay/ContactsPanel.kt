package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.BitmapShader
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.Shader
import android.graphics.drawable.BitmapDrawable
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.provider.ContactsContract
import android.view.Gravity
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import java.io.File
import kotlin.concurrent.thread

object ContactsPanel {

    data class Contact(
        val id: Long,
        val name: String,
        val lookupKey: String?,
        val photoUri: String?,
        val numbers: List<Pair<String, String>>,
        val starred: Boolean
    )

    private var contactsData = listOf<Contact>()
    private var detailOverlay: FrameLayout? = null

    fun create(ctx: Context): View {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }

        val root = FrameLayout(ctx).apply {
            setBackgroundColor(0xFF111111.toInt())
        }

        val mainContent = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
        }

        val header = buildHeader(ctx)
        mainContent.addView(header)

        val toolbar = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(10, 6, 10, 6)
            gravity = Gravity.CENTER_VERTICAL
        }

        val searchInput = EditText(ctx).apply {
            hint = "\uD83D\uDD0D  Search contacts"
            setHintTextColor(0xFF555555.toInt())
            setTextColor(0xFFDDDDDD.toInt())
            textSize = 14f
            setBackgroundColor(0xFF1E1E1E.toInt())
            setPadding(14, 10, 14, 10)
            setSingleLine(true)
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            imeOptions = EditorInfo.IME_ACTION_SEARCH
        }

        val exportBtn = buildBtn(ctx, "Export") {
            exportContacts(ctx)
        }
        exportBtn.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { setMargins(8, 0, 0, 0) }

        toolbar.addView(searchInput)
        toolbar.addView(exportBtn)
        mainContent.addView(toolbar)

        val countLabel = TextView(ctx).apply {
            setTextColor(0xFF777777.toInt())
            textSize = 12f
            setPadding(14, 6, 14, 6)
        }
        mainContent.addView(countLabel)

        val listContainer = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }
        val scroll = ScrollView(ctx).apply { addView(listContainer) }

        fun showList(filter: String = "") {
            val filtered = if (filter.isBlank()) contactsData
            else contactsData.filter {
                it.name.contains(filter, true) || it.numbers.any { (_, n) -> n.contains(filter, true) }
            }

            val starred = filtered.filter { it.starred }.sortedBy { it.name.lowercase() }
            val nonStarred = filtered.filter { !it.starred }.sortedBy { it.name.lowercase() }

            countLabel.text = "${starred.size} \u2605 \u00B7 ${filtered.size} contacts"

            listContainer.removeAllViews()
            val dividerColor = 0xFF222222.toInt()

            if (starred.isNotEmpty()) {
                val favHeader = TextView(ctx).apply {
                    text = "  \u2605 Favorites"
                    setTextColor(0xFFCC8800.toInt())
                    textSize = 13f
                    setPadding(dp(14), dp(10), dp(14), dp(6))
                    setBackgroundColor(0xFF161616.toInt())
                }
                listContainer.addView(favHeader)

                for (c in starred) {
                    val row = buildContactRow(ctx, c)
                    listContainer.addView(row)
                    val div = View(ctx).apply {
                        layoutParams = LinearLayout.LayoutParams(
                            LinearLayout.LayoutParams.MATCH_PARENT, 1
                        )
                        setBackgroundColor(dividerColor)
                    }
                    listContainer.addView(div)
                }
            }

            var lastInitial = '\u0000'
            for (c in nonStarred) {
                val initial = c.name.firstOrNull()?.uppercaseChar() ?: '#'
                if (initial != lastInitial) {
                    lastInitial = initial
                    val sectionLabel = TextView(ctx).apply {
                        text = "  $initial"
                        setTextColor(0xFF888888.toInt())
                        textSize = 13f
                        setPadding(dp(14), dp(10), dp(14), dp(6))
                        setBackgroundColor(0xFF161616.toInt())
                    }
                    listContainer.addView(sectionLabel)
                }

                val row = buildContactRow(ctx, c)
                listContainer.addView(row)

                val div = View(ctx).apply {
                    layoutParams = LinearLayout.LayoutParams(
                        LinearLayout.LayoutParams.MATCH_PARENT, 1
                    )
                    setBackgroundColor(dividerColor)
                }
                listContainer.addView(div)
            }

            if (filtered.isEmpty()) {
                val empty = TextView(ctx).apply {
                    text = if (filter.isBlank()) "No contacts" else "No match"
                    setTextColor(0xFF666666.toInt())
                    textSize = 14f
                    gravity = Gravity.CENTER
                    setPadding(0, 48, 0, 0)
                }
                listContainer.addView(empty)
            }
        }

        var swipeRefresh: SwipeRefreshLayout? = null
        val sr = SwipeRefreshLayout(ctx).apply {
            addView(scroll)
            setColorSchemeColors(0xFF888888.toInt(), 0xFFAAAAAA.toInt())
            setProgressBackgroundColorSchemeColor(0xFF222222.toInt())
            setOnRefreshListener {
                thread(name = "gafam-load-contacts", isDaemon = true) {
                    loadContacts(ctx)
                    (ctx as? android.app.Activity)?.runOnUiThread {
                        showList(searchInput.text.toString())
                        swipeRefresh?.isRefreshing = false
                    }
                }
            }
        }
        swipeRefresh = sr
        mainContent.addView(sr, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f
        ))

        root.addView(mainContent)

        val overlay = FrameLayout(ctx).apply {
            setBackgroundColor(0xCC000000.toInt())
            visibility = View.GONE
            setOnClickListener { visibility = View.GONE }
        }
        detailOverlay = overlay
        root.addView(overlay, FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT
        ))

        searchInput.addTextChangedListener(object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) { showList(s?.toString() ?: "") }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        })

        thread(name = "gafam-load-contacts", isDaemon = true) {
            loadContacts(ctx)
            (ctx as? android.app.Activity)?.runOnUiThread { showList(searchInput.text.toString()) }
        }

        return root
    }

    fun onBackPressed(): Boolean {
        val overlay = detailOverlay
        if (overlay?.visibility == View.VISIBLE) {
            overlay.visibility = View.GONE
            return true
        }
        return false
    }

    private fun buildHeader(ctx: Context): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        return LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(dp(14), dp(10), dp(14), dp(10))
            setBackgroundColor(0xFF181818.toInt())
            gravity = Gravity.CENTER_VERTICAL
            val tv = TextView(ctx).apply {
                text = "\uD83D\uDC64  Contacts"
                setTextColor(0xFFDDDDDD.toInt())
                textSize = 16f
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            }
            addView(tv)
        }
    }

    private fun buildContactRow(ctx: Context, contact: Contact): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }
        val row = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(dp(14), dp(12), dp(10), dp(12))
            gravity = Gravity.CENTER_VERTICAL
            setBackgroundColor(0xFF111111.toInt())
            setOnClickListener {
                showContactDetail(ctx, contact)
            }
        }

        val avatar = buildAvatarForContact(ctx, contact, dp(40))
        row.addView(avatar)

        val textCol = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            setPadding(dp(12), 0, dp(8), 0)
        }
        val nameTv = TextView(ctx).apply {
            text = contact.name
            setTextColor(0xFFDDDDDD.toInt())
            textSize = 15f
            maxLines = 1
        }
        val phoneTv = TextView(ctx).apply {
            text = contact.numbers.firstOrNull()?.second ?: ""
            setTextColor(0xFF888888.toInt())
            textSize = 12f
        }
        textCol.addView(nameTv)
        textCol.addView(phoneTv)

        val primaryNumber = contact.numbers.firstOrNull()?.second ?: ""

        val callBtn = buildMiniBtn(ctx, "\uD83D\uDCDE") {
            if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.CALL_PHONE) == PackageManager.PERMISSION_GRANTED) {
                ctx.startActivity(Intent(Intent.ACTION_CALL, Uri.parse("tel:$primaryNumber")))
            } else {
                ctx.startActivity(Intent(Intent.ACTION_DIAL, Uri.parse("tel:$primaryNumber")))
            }
        }
        callBtn.setTextColor(0xFF33AA33.toInt())
        callBtn.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { setMargins(dp(4), 0, dp(4), 0) }

        val smsBtn = buildMiniBtn(ctx, "SMS") {
            val intent = Intent(Intent.ACTION_VIEW).apply {
                data = Uri.parse("sms:$primaryNumber")
            }
            ctx.startActivity(intent)
        }
        smsBtn.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { setMargins(dp(4), 0, dp(4), 0) }

        val copyBtn = buildMiniBtn(ctx, "Copy") {
            val clip = android.content.ClipData.newPlainText("phone", primaryNumber)
            (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                .setPrimaryClip(clip)
            Toast.makeText(ctx, "Copied", Toast.LENGTH_SHORT).show()
        }

        row.addView(textCol)
        row.addView(callBtn)
        row.addView(smsBtn)
        row.addView(copyBtn)
        return row
    }

    private fun showContactDetail(ctx: Context, contact: Contact) {
        val overlay = detailOverlay ?: return
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }

        overlay.removeAllViews()

        val cardScroll = ScrollView(ctx)
        val card = buildDetailCard(ctx, contact, overlay)

        card.setOnClickListener { }
        cardScroll.addView(card)

        val cardParams = FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.WRAP_CONTENT
        ).apply {
            gravity = Gravity.CENTER
            setMargins(dp(24), dp(40), dp(24), dp(40))
        }

        overlay.addView(cardScroll, cardParams)
        overlay.visibility = View.VISIBLE
    }

    private fun buildDetailCard(ctx: Context, contact: Contact, overlay: FrameLayout): LinearLayout {
        val dp = { v: Int -> (v * ctx.resources.displayMetrics.density).toInt() }

        val card = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(20), dp(20), dp(20), dp(20))
            background = GradientDrawable().apply {
                setColor(0xFF222222.toInt())
                cornerRadius = dp(16).toFloat()
            }
            setOnClickListener { }
        }

        val avatarContainer = LinearLayout(ctx).apply {
            gravity = Gravity.CENTER
        }
        val avatarView = if (contact.photoUri != null) {
            buildAvatarForContact(ctx, contact, dp(80))
        } else {
            buildAvatar(ctx, contact.name.take(1).uppercase(), 0xFF555555.toInt(), dp(80))
        }
        avatarContainer.addView(avatarView)
        card.addView(avatarContainer)

        val nameTv = TextView(ctx).apply {
            text = contact.name
            setTextColor(0xFFDDDDDD.toInt())
            textSize = 18f
            setTypeface(null, android.graphics.Typeface.BOLD)
            gravity = Gravity.CENTER
            setPadding(0, dp(12), 0, dp(4))
        }
        card.addView(nameTv)

        val starBtn = TextView(ctx).apply {
            text = if (contact.starred) "\u2605" else "\u2606"
            setTextColor(if (contact.starred) 0xFFCC8800.toInt() else 0xFF666666.toInt())
            textSize = 20f
            gravity = Gravity.CENTER
            setPadding(0, 0, 0, dp(12))
            setOnClickListener {
                Toast.makeText(ctx, "Star toggle requires WRITE_CONTACTS permission", Toast.LENGTH_SHORT).show()
            }
        }
        card.addView(starBtn)

        card.addView(View(ctx).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1)
            setBackgroundColor(0xFF333333.toInt())
        })

        for ((type, number) in contact.numbers) {
            val numRow = LinearLayout(ctx).apply {
                orientation = LinearLayout.HORIZONTAL
                setPadding(0, dp(10), 0, dp(10))
                gravity = Gravity.CENTER_VERTICAL
            }

            val typeTv = TextView(ctx).apply {
                text = type
                setTextColor(0xFF888888.toInt())
                textSize = 11f
                layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply { setMargins(0, 0, dp(6), 0) }
            }

            val numTv = TextView(ctx).apply {
                text = number
                setTextColor(0xFFDDDDDD.toInt())
                textSize = 14f
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            }

            val callBtn = TextView(ctx).apply {
                text = "Call"
                setTextColor(0xFF33AA33.toInt())
                textSize = 12f
                setPadding(dp(6), dp(3), dp(6), dp(3))
                background = GradientDrawable().apply {
                    setStroke(1, 0xFF33AA33.toInt())
                    cornerRadius = dp(4).toFloat()
                }
                setOnClickListener {
                    if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.CALL_PHONE) == PackageManager.PERMISSION_GRANTED) {
                        ctx.startActivity(Intent(Intent.ACTION_CALL, Uri.parse("tel:$number")))
                    } else {
                        ctx.startActivity(Intent(Intent.ACTION_DIAL, Uri.parse("tel:$number")))
                    }
                    overlay.visibility = View.GONE
                }
            }

            val smsBtn = TextView(ctx).apply {
                text = "SMS"
                setTextColor(0xFFAAAAAA.toInt())
                textSize = 12f
                setPadding(dp(6), dp(3), dp(6), dp(3))
                background = GradientDrawable().apply {
                    setStroke(1, 0xFF444444.toInt())
                    cornerRadius = dp(4).toFloat()
                }
                setOnClickListener {
                    ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("sms:$number")))
                    overlay.visibility = View.GONE
                }
            }

            numRow.addView(typeTv)
            numRow.addView(numTv)
            numRow.addView(callBtn)
            numRow.addView(smsBtn)
            card.addView(numRow)

            card.addView(View(ctx).apply {
                layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 1)
                setBackgroundColor(0xFF2A2A2A.toInt())
            })
        }

        val closeBtn = TextView(ctx).apply {
            text = "Close"
            setTextColor(0xFFAAAAAA.toInt())
            textSize = 14f
            gravity = Gravity.CENTER
            setPadding(0, dp(16), 0, 0)
            setOnClickListener { overlay.visibility = View.GONE }
        }
        card.addView(closeBtn)

        return card
    }

    private fun loadContacts(ctx: Context) {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_CONTACTS)
            != PackageManager.PERMISSION_GRANTED) return

        val list = mutableListOf<Contact>()
        val contactCursor = ctx.contentResolver.query(
            ContactsContract.Contacts.CONTENT_URI,
            arrayOf(
                ContactsContract.Contacts._ID,
                ContactsContract.Contacts.DISPLAY_NAME,
                ContactsContract.Contacts.LOOKUP_KEY,
                ContactsContract.Contacts.PHOTO_THUMBNAIL_URI,
                ContactsContract.Contacts.STARRED
            ),
            null, null,
            ContactsContract.Contacts.DISPLAY_NAME + " ASC"
        )
        contactCursor?.use { cc ->
            val idIdx = cc.getColumnIndex(ContactsContract.Contacts._ID)
            val nameIdx = cc.getColumnIndex(ContactsContract.Contacts.DISPLAY_NAME)
            val lookupIdx = cc.getColumnIndex(ContactsContract.Contacts.LOOKUP_KEY)
            val photoIdx = cc.getColumnIndex(ContactsContract.Contacts.PHOTO_THUMBNAIL_URI)
            val starredIdx = cc.getColumnIndex(ContactsContract.Contacts.STARRED)

            while (cc.moveToNext()) {
                val contactId = cc.getLong(idIdx)
                val name = cc.getString(nameIdx) ?: "Unknown"
                val lookupKey = cc.getString(lookupIdx)
                val photoUri = cc.getString(photoIdx)
                val starred = cc.getInt(starredIdx) == 1

                val numbers = mutableListOf<Pair<String, String>>()
                ctx.contentResolver.query(
                    ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                    arrayOf(
                        ContactsContract.CommonDataKinds.Phone.NUMBER,
                        ContactsContract.CommonDataKinds.Phone.TYPE,
                        ContactsContract.CommonDataKinds.Phone.LABEL
                    ),
                    "${ContactsContract.CommonDataKinds.Phone.CONTACT_ID} = ? AND ${ContactsContract.CommonDataKinds.Phone.MIMETYPE} = ?",
                    arrayOf(contactId.toString(), ContactsContract.CommonDataKinds.Phone.CONTENT_ITEM_TYPE),
                    null
                )?.use { phoneCursor ->
                    val numIdx = phoneCursor.getColumnIndex(ContactsContract.CommonDataKinds.Phone.NUMBER)
                    val typeIdx = phoneCursor.getColumnIndex(ContactsContract.CommonDataKinds.Phone.TYPE)
                    val labelIdx = phoneCursor.getColumnIndex(ContactsContract.CommonDataKinds.Phone.LABEL)
                    while (phoneCursor.moveToNext()) {
                        val number = phoneCursor.getString(numIdx)?.filter { it.isDigit() } ?: ""
                        if (number.isNotBlank() && number.length >= 7) {
                            val typeCode = phoneCursor.getInt(typeIdx)
                            val customLabel = phoneCursor.getString(labelIdx)
                            val typeLabel = phoneTypeLabel(typeCode, customLabel)
                            numbers.add(typeLabel to number)
                        }
                    }
                }

                if (numbers.isNotEmpty()) {
                    list.add(Contact(contactId, name, lookupKey, photoUri, numbers, starred))
                }
            }
        }
        contactsData = list
    }

    private fun phoneTypeLabel(typeCode: Int, customLabel: String?): String {
        return when (typeCode) {
            ContactsContract.CommonDataKinds.Phone.TYPE_MOBILE -> "Mobile"
            ContactsContract.CommonDataKinds.Phone.TYPE_HOME -> "Home"
            ContactsContract.CommonDataKinds.Phone.TYPE_WORK -> "Work"
            ContactsContract.CommonDataKinds.Phone.TYPE_MAIN -> "Main"
            ContactsContract.CommonDataKinds.Phone.TYPE_FAX_WORK -> "Work Fax"
            ContactsContract.CommonDataKinds.Phone.TYPE_FAX_HOME -> "Home Fax"
            ContactsContract.CommonDataKinds.Phone.TYPE_PAGER -> "Pager"
            ContactsContract.CommonDataKinds.Phone.TYPE_OTHER -> "Other"
            ContactsContract.CommonDataKinds.Phone.TYPE_CUSTOM -> customLabel ?: "Custom"
            else -> "Other"
        }
    }

    private fun buildAvatarForContact(ctx: Context, contact: Contact, sizePx: Int): TextView {
        val letter = contact.name.take(1).uppercase()
        val tv = buildAvatar(ctx, letter, 0xFF555555.toInt(), sizePx)

        val photoUri = contact.photoUri
        if (photoUri != null) {
            thread(name = "gafam-contact-photo", isDaemon = true) {
                try {
                    val stream = ctx.contentResolver.openInputStream(Uri.parse(photoUri))
                    val bitmap = BitmapFactory.decodeStream(stream)
                    stream?.close()
                    if (bitmap != null) {
                        val circular = circularBitmapDrawable(ctx, bitmap, sizePx)
                        (ctx as? android.app.Activity)?.runOnUiThread {
                            tv.background = circular
                            tv.text = ""
                        }
                    }
                } catch (_: Exception) {}
            }
        }
        return tv
    }

    private fun circularBitmapDrawable(ctx: Context, bitmap: Bitmap, sizePx: Int): BitmapDrawable {
        val output = Bitmap.createBitmap(sizePx, sizePx, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(output)
        val paint = Paint(Paint.ANTI_ALIAS_FLAG)
        val shader = BitmapShader(bitmap, Shader.TileMode.CLAMP, Shader.TileMode.CLAMP)
        val scale = sizePx.toFloat() / minOf(bitmap.width, bitmap.height).toFloat()
        val matrix = Matrix()
        matrix.setScale(scale, scale)
        matrix.postTranslate(
            (sizePx - bitmap.width * scale) / 2f,
            (sizePx - bitmap.height * scale) / 2f
        )
        shader.setLocalMatrix(matrix)
        paint.shader = shader
        canvas.drawCircle(sizePx / 2f, sizePx / 2f, sizePx / 2f, paint)
        return BitmapDrawable(ctx.resources, output)
    }

    fun buildAvatar(ctx: Context, letter: String, bgColor: Int, sizePx: Int? = null): TextView {
        val size = sizePx ?: (40 * ctx.resources.displayMetrics.density).toInt()
        return TextView(ctx).apply {
            text = letter
            setTextColor(0xFF111111.toInt())
            setBackgroundColor(bgColor)
            gravity = Gravity.CENTER
            textSize = (size / 2.0f).coerceAtMost(18f)
            width = size
            height = size
            background = GradientDrawable().apply {
                setColor(bgColor)
                shape = GradientDrawable.OVAL
            }
        }
    }

    private fun escapeVCard(s: String): String {
        return s.replace("\\", "\\\\")
            .replace(",", "\\,")
            .replace(";", "\\;")
            .replace("\n", "\\n")
    }

    private fun exportContacts(ctx: Context) {
        thread(name = "gafam-export-contacts", isDaemon = true) {
            try {
                val vcf = buildString {
                    for (c in contactsData) {
                        append("BEGIN:VCARD\nVERSION:3.0\n")
                        append("FN:${escapeVCard(c.name)}\n")
                        for ((type, number) in c.numbers) {
                            append("TEL;TYPE=${escapeVCard(type)}:${escapeVCard(number)}\n")
                        }
                        append("END:VCARD\n")
                    }
                }
                val file = File(ctx.cacheDir, "gafam_contacts.vcf")
                file.writeText(vcf)

                val shareIntent = Intent(Intent.ACTION_SEND).apply {
                    type = "text/x-vcard"
                    putExtra(Intent.EXTRA_STREAM,
                        FileProvider.getUriForFile(ctx,
                            "${ctx.packageName}.fileprovider", file))
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                }
                (ctx as? android.app.Activity)?.runOnUiThread {
                    ctx.startActivity(Intent.createChooser(shareIntent, "Export contacts"))
                }
            } catch (e: Exception) {
                (ctx as? android.app.Activity)?.runOnUiThread {
                    Toast.makeText(ctx, "Export failed: ${e.message}", Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    private fun buildBtn(ctx: Context, text: String, onClick: () -> Unit): TextView {
        return TextView(ctx).apply {
            this.text = text
            setTextColor(0xFF111111.toInt())
            textSize = 13f
            setPadding(14, 8, 14, 8)
            gravity = Gravity.CENTER
            background = GradientDrawable().apply {
                setColor(0xFFAAAAAA.toInt())
                cornerRadius = 6f * ctx.resources.displayMetrics.density
            }
            setOnClickListener { onClick() }
        }
    }

    private fun buildMiniBtn(ctx: Context, text: String, onClick: () -> Unit): TextView {
        return TextView(ctx).apply {
            this.text = text
            setTextColor(0xFFBBBBBB.toInt())
            textSize = 11f
            setPadding(10, 6, 10, 6)
            gravity = Gravity.CENTER
            background = GradientDrawable().apply {
                setStroke(1, 0xFF444444.toInt())
                cornerRadius = 4f * ctx.resources.displayMetrics.density
            }
            setOnClickListener { onClick() }
        }
    }

}
