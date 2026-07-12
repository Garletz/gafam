/** Client-side OTP/code detection (mirrors VPC suparna.DetectCodes). */
export function detectSmsCodes(text: string): string[] {
  if (!text?.trim()) return [];
  const lower = text.toLowerCase();
  const hasKeyword = /code|otp|pin|verification|confirm|gafam|impulsion|accès|acces/i.test(lower);
  const seen = new Set<string>();
  const out: string[] = [];

  const add = (raw: string) => {
    const c = raw.replace(/[\s-]/g, '').toUpperCase();
    if (c.length < 4 || c.length > 10) return;
    if (c.length === 4 && c >= '1900' && c <= '2099') return;
    if (seen.has(c)) return;
    seen.add(c);
    out.push(c);
  };

  if (hasKeyword) {
    const time = text.match(/\b([01]?\d|2[0-3])[:h]([0-5]\d)\b/g);
    if (time) time.forEach((t) => add(t.replace('h', ':')));
  }

  const nums = text.match(/(?:^|[\s:：\-—])((?:\d[\s-]?){4,8}\d)(?:[\s.,!?]|$)/g);
  if (nums) {
    for (const m of nums) {
      const d = m.match(/(\d[\s-]?)+/);
      if (d) add(d[0]);
    }
  }

  if (hasKeyword) {
    const alpha = text.toUpperCase().match(/\b[A-Z0-9]{4,8}\b/g);
    if (alpha) {
      for (const a of alpha) {
        if (!['GAFAM', 'CODE', 'HTTP', 'HTTPS', 'TRUE', 'SMS', 'AUTH'].includes(a)) add(a);
      }
    }
  }

  return out.sort();
}
