const DEFAULT_AUTH_REDIRECT = '/dashboard'

export function sanitizeInternalRedirect(
  path: string | null | undefined,
  fallback: string = DEFAULT_AUTH_REDIRECT
): string {
  const normalizedFallback = pathIsSafe(fallback) ? fallback : DEFAULT_AUTH_REDIRECT

  if (!path) {
    return normalizedFallback
  }

  const trimmed = path.trim()
  if (!pathIsSafe(trimmed)) {
    return normalizedFallback
  }

  return trimmed
}

function pathIsSafe(path: string): boolean {
  return (
    path.startsWith('/') &&
    !path.startsWith('//') &&
    !path.includes('://') &&
    !path.includes('\n') &&
    !path.includes('\r')
  )
}
