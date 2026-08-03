export interface PeakRateFields {
  peak_rate_enabled?: boolean
  peak_start?: string | null
  peak_end?: string | null
  peak_rate_multiplier?: number | null
}

export function peakRateMultiplierLabel(value?: number | null): string {
  const multiplier = value ?? 1
  return `×${Number(multiplier.toPrecision(10))}`
}

export function serverTimezoneLabel(serverUTCOffset?: string | null): string {
  const offset = (serverUTCOffset || '').trim()
  return offset ? `UTC${offset}` : 'server time'
}

export function formatPeakRateWindow(fields: PeakRateFields, timezoneLabel?: string | null): string {
  if (!fields.peak_rate_enabled || !fields.peak_start || !fields.peak_end) {
    return ''
  }
  let tz = (timezoneLabel || '').trim()
  if (tz.startsWith('+') || tz.startsWith('-')) {
    tz = `UTC${tz}`
  }
  const window = `${fields.peak_start}-${fields.peak_end}`
  return tz ? `${window} (${tz})` : window
}
