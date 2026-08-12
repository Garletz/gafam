package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.provider.ContactsContract
import android.view.Gravity
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import java.io.File
import kotlin.concurrent.thread

object ContactsPanel {

    data class Contact(val name: String, val number: String)

    private var contactsData = listOf<Contact>()

    fun create(ctx: Context): View {
        val root = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(0xFF111111.toInt())
        }

        val header = buildHeader(ctx)
        root.addView(header)

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
        root.addView(toolbar)

        val countLabel = TextView(ctx).apply {
            setTextColor(0xFF777777.toInt())
            textSize = 12f
            setPadding(14, 6, 14, 6)
        }
        root.addView(countLabel)

        val listContainer = LinearLayout(ctx).apply { orientation = LinearLayout.VERTICAL }
        val scroll = ScrollView(ctx).apply { addView(listContainer) }
        root.addView(scroll, LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f
        ))

        fun showList(filter: String = "") {
            val filtered = if (filter.isBlank()) contactsData
            else contactsData.filter {
                it.name.contains(filter, true) || it.number.contains(filter, true)
            }
            countLabel.text = "${filtered.size} contacts"

            listContainer.removeAllViews()
            val dividerColor = 0xFF222222.toInt()

            var lastInitial = '\u0000'
            for (c in filtered) {
                val initial = c.name.firstOrNull()?.uppercaseChar() ?: '#'
                if (initial != lastInitial) {
                    lastInitial = initial
                    val sectionLabel = TextView(ctx).apply {
                        text = "  $initial"
                        setTextColor(0xFF888888.toInt())
                        textSize = 13f
                        setPadding(14, 10, 14, 6)
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

    private fun buildHeader(ctx: Context): LinearLayout {
        return LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(14, 10, 14, 10)
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
        val row = LinearLayout(ctx).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(14, 12, 10, 12)
            gravity = Gravity.CENTER_VERTICAL
            setBackgroundColor(0xFF111111.toInt())
            setOnClickListener {
                showContactDetail(ctx, contact)
            }
        }

        val avatar = buildAvatar(ctx, contact.name.take(1).uppercase(), 0xFF555555.toInt())
        row.addView(avatar)

        val textCol = LinearLayout(ctx).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
            setPadding(12, 0, 8, 0)
        }
        val nameTv = TextView(ctx).apply {
            text = contact.name
            setTextColor(0xFFDDDDDD.toInt())
            textSize = 15f
            maxLines = 1
        }
        val phoneTv = TextView(ctx).apply {
            text = contact.number
            setTextColor(0xFF888888.toInt())
            textSize = 12f
        }
        textCol.addView(nameTv)
        textCol.addView(phoneTv)

        val smsBtn = buildMiniBtn(ctx, "SMS") {
            val intent = Intent(Intent.ACTION_VIEW).apply {
                data = android.net.Uri.parse("sms:${contact.number}")
            }
            ctx.startActivity(intent)
        }
        smsBtn.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { setMargins(4, 0, 4, 0) }

        val copyBtn = buildMiniBtn(ctx, "Copy") {
            val clip = android.content.ClipData.newPlainText("phone", contact.number)
            (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                .setPrimaryClip(clip)
            android.widget.Toast.makeText(ctx, "Copied", android.widget.Toast.LENGTH_SHORT).show()
        }

        row.addView(textCol)
        row.addView(smsBtn)
        row.addView(copyBtn)
        return row
    }

    private fun showContactDetail(ctx: Context, contact: Contact) {
        android.app.AlertDialog.Builder(ctx)
            .setTitle(contact.name)
            .setMessage(contact.number)
            .setPositiveButton("SMS") { _, _ ->
                val intent = Intent(Intent.ACTION_VIEW).apply {
                    data = android.net.Uri.parse("sms:${contact.number}")
                }
                ctx.startActivity(intent)
            }
            .setNeutralButton("Copy") { _, _ ->
                val clip = android.content.ClipData.newPlainText("phone", contact.number)
                (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                    .setPrimaryClip(clip)
                android.widget.Toast.makeText(ctx, "Copied", android.widget.Toast.LENGTH_SHORT).show()
            }
            .setNegativeButton("Close", null)
            .show()
    }

    private fun loadContacts(ctx: Context) {
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.READ_CONTACTS)
            != PackageManager.PERMISSION_GRANTED) return

        val list = mutableListOf<Contact>()
        val cursor = ctx.contentResolver.query(
            ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
            arrayOf(
                ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME,
                ContactsContract.CommonDataKinds.Phone.NUMBER
            ),
            null, null,
            ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME + " ASC"
        )
        cursor?.use {
            val nameIdx = it.getColumnIndex(ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME)
            val phoneIdx = it.getColumnIndex(ContactsContract.CommonDataKinds.Phone.NUMBER)
            while (it.moveToNext()) {
                val name = it.getString(nameIdx) ?: "Unknown"
                val number = it.getString(phoneIdx)?.filter { c -> c.isDigit() } ?: ""
                if (number.isNotBlank() && number.length >= 7) {
                    list.add(Contact(name, number))
                }
            }
        }
        contactsData = list
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
                        append("TEL:${escapeVCard(c.number)}\n")
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
                    android.widget.Toast.makeText(ctx, "Export failed: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    fun buildAvatar(ctx: Context, letter: String, bgColor: Int): TextView {
        val size = 40.dp(ctx)
        return TextView(ctx).apply {
            text = letter
            setTextColor(0xFF111111.toInt())
            setBackgroundColor(bgColor)
            gravity = Gravity.CENTER
            textSize = 16f
            width = size
            height = size
            background = android.graphics.drawable.GradientDrawable().apply {
                setColor(bgColor)
                shape = android.graphics.drawable.GradientDrawable.OVAL
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
            background = android.graphics.drawable.GradientDrawable().apply {
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
            background = android.graphics.drawable.GradientDrawable().apply {
                setStroke(1, 0xFF444444.toInt())
                cornerRadius = 4f * ctx.resources.displayMetrics.density
            }
            setOnClickListener { onClick() }
        }
    }

    private fun Int.dp(ctx: Context): Int = (this * ctx.resources.displayMetrics.density).toInt()
    private fun Float.dpf(ctx: Context): Float = this * ctx.resources.displayMetrics.density
}
