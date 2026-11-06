export function releaseDownloadName(disposition: string | null, releaseId: string): string {
  let name = ''
  const encoded = disposition?.match(/filename\*\s*=\s*UTF-8''([^;]+)/i)?.[1]
  if (encoded) {
    try { name = decodeURIComponent(encoded.trim()) } catch { /* 回退到普通文件名。 */ }
  }
  if (!name) {
    const plain = disposition?.match(/filename\s*=\s*(?:"([^"]+)"|([^;\s]+))/i)
    name = plain?.[1] ?? plain?.[2] ?? ''
  }
  const leaf = name.split(/[\\/]/).pop()?.replace(/[\x00-\x1f\x7f]/g, '').trim() ?? ''
  return leaf && leaf !== '.' && leaf !== '..' && leaf.length <= 180
    ? leaf : 'fde-release-' + releaseId.replace(/[^a-zA-Z0-9_-]/g, '_') + '.zip'
}
