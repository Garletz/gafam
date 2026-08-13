// Minimal, safe markdown renderer — black & white house style.
// HTML is escaped FIRST, then markdown patterns are applied: no raw HTML can
// ever pass through (vault notes contain fetched web content).
// Supports: headings, bold/italic, inline + fenced code, lists, tables,
// links (http/https only), hr, paragraphs.

function esc(s: string): string {
	return s
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');
}

function inline(s: string): string {
	let out = esc(s);
	out = out.replace(/`([^`]+)`/g, (_m, c) => `<code>${c}</code>`);
	out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
	out = out.replace(/(?<!\*)\*([^*\n]+)\*(?!\*)/g, '<em>$1</em>');
	out = out.replace(
		/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g,
		'<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>'
	);
	return out;
}

function table(lines: string[]): string {
	// lines: rows of "| a | b |", second line is the |---| separator
	const rows = lines.map((l) =>
		l
			.trim()
			.replace(/^\||\|$/g, '')
			.split('|')
			.map((c) => c.trim())
	);
	const head = rows[0];
	const body = rows.slice(2);
	let h = '<table><thead><tr>';
	for (const c of head) h += `<th>${inline(c)}</th>`;
	h += '</tr></thead><tbody>';
	for (const r of body) {
		h += '<tr>';
		for (const c of r) h += `<td>${inline(c)}</td>`;
		h += '</tr>';
	}
	return h + '</tbody></table>';
}

export function renderMarkdown(src: string): string {
	const lines = (src || '').replace(/\r\n/g, '\n').split('\n');
	const out: string[] = [];
	let i = 0;
	let listType: 'ul' | 'ol' | null = null;

	const closeList = () => {
		if (listType) {
			out.push(`</${listType}>`);
			listType = null;
		}
	};

	while (i < lines.length) {
		const line = lines[i];

		// Fenced code block
		if (line.trimStart().startsWith('```')) {
			closeList();
			const buf: string[] = [];
			i++;
			while (i < lines.length && !lines[i].trimStart().startsWith('```')) {
				buf.push(lines[i]);
				i++;
			}
			i++; // closing fence
			out.push(`<pre><code>${esc(buf.join('\n'))}</code></pre>`);
			continue;
		}

		// Table (current line starts with |, next is separator)
		if (
			line.trim().startsWith('|') &&
			i + 1 < lines.length &&
			/^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1]) &&
			lines[i + 1].includes('-')
		) {
			closeList();
			const buf: string[] = [line, lines[i + 1]];
			i += 2;
			while (i < lines.length && lines[i].trim().startsWith('|')) {
				buf.push(lines[i]);
				i++;
			}
			out.push(table(buf));
			continue;
		}

		// Heading
		const hm = line.match(/^(#{1,4})\s+(.*)$/);
		if (hm) {
			closeList();
			const level = hm[1].length;
			out.push(`<h${level}>${inline(hm[2])}</h${level}>`);
			i++;
			continue;
		}

		// Horizontal rule
		if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
			closeList();
			out.push('<hr>');
			i++;
			continue;
		}

		// Unordered list
		const ulm = line.match(/^\s*[-*•]\s+(.*)$/);
		if (ulm) {
			if (listType !== 'ul') {
				closeList();
				out.push('<ul>');
				listType = 'ul';
			}
			out.push(`<li>${inline(ulm[1])}</li>`);
			i++;
			continue;
		}

		// Ordered list
		const olm = line.match(/^\s*\d+[.)]\s+(.*)$/);
		if (olm) {
			if (listType !== 'ol') {
				closeList();
				out.push('<ol>');
				listType = 'ol';
			}
			out.push(`<li>${inline(olm[1])}</li>`);
			i++;
			continue;
		}

		// Blank line
		if (line.trim() === '') {
			closeList();
			i++;
			continue;
		}

		// Paragraph
		closeList();
		out.push(`<p>${inline(line)}</p>`);
		i++;
	}
	closeList();
	return out.join('\n');
}
