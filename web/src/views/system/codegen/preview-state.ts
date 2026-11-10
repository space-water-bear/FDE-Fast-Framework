import type { GenPreview } from '@/api/codegen'

export function requestFingerprint(request: unknown): string {
  return JSON.stringify(request)
}

export function canApplyPreview(preview: GenPreview | null, expected: string, current: string): boolean {
  return !!preview?.ok && !!preview.batchId && expected === current && !(preview.diff?.conflicts?.length)
}
