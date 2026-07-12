import { describe, it, expect, beforeEach, vi } from 'vitest'
import { isPrivateIp, getEntry, formatGeoLabel, fetchOne, fetchBatch } from '../ipGeoLookup'

describe('isPrivateIp', () => {
  it('identifies private/reserved IPv4 ranges', () => {
    expect(isPrivateIp('10.0.0.1')).toBe(true)
    expect(isPrivateIp('127.0.0.1')).toBe(true)
    expect(isPrivateIp('192.168.1.1')).toBe(true)
    expect(isPrivateIp('172.16.0.1')).toBe(true)
    expect(isPrivateIp('172.31.255.255')).toBe(true)
    expect(isPrivateIp('169.254.1.1')).toBe(true)
  })

  it('does not flag public IPv4 addresses', () => {
    expect(isPrivateIp('8.8.8.8')).toBe(false)
    expect(isPrivateIp('172.32.0.1')).toBe(false)
    expect(isPrivateIp('121.35.47.43')).toBe(false)
  })

  it('identifies private/reserved IPv6 addresses', () => {
    expect(isPrivateIp('::1')).toBe(true)
    expect(isPrivateIp('fe80::1')).toBe(true)
    expect(isPrivateIp('fe90::1')).toBe(true)
    expect(isPrivateIp('febf::1')).toBe(true)
    expect(isPrivateIp('fc00::1')).toBe(true)
    expect(isPrivateIp('fd00::1')).toBe(true)
    expect(isPrivateIp('fdff::1')).toBe(true)
  })

  it('does not overmatch public IPv6 addresses near private ranges', () => {
    expect(isPrivateIp('fec0::1')).toBe(false)
    expect(isPrivateIp('fbff::1')).toBe(false)
    expect(isPrivateIp('fe7f::1')).toBe(false)
  })
})

describe('getEntry', () => {
  it('returns an idle entry for an IP that has never been fetched', () => {
    expect(getEntry('203.0.113.9')).toEqual({ status: 'idle' })
  })
})

describe('formatGeoLabel', () => {
  it('joins country/region/city with a separator', () => {
    expect(formatGeoLabel({ countryCode: 'CN', region: 'Guangdong', city: 'Shenzhen' })).toBe('CN · Guangdong · Shenzhen')
  })

  it('skips missing fields', () => {
    expect(formatGeoLabel({ countryCode: 'CN' })).toBe('CN')
    expect(formatGeoLabel({ countryCode: 'US', region: 'Massachusetts' })).toBe('US · Massachusetts')
  })
})

describe('fetchOne', () => {
  beforeEach(() => {
    localStorage.clear()
    global.fetch = vi.fn()
  })

  it('marks a private IP without making a network request', async () => {
    await fetchOne('192.168.50.1')
    expect(getEntry('192.168.50.1')).toEqual({ status: 'private' })
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('fetches and stores a successful geolocation result', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({
        ip: '121.35.47.43',
        country_code: 'CN',
        region: 'Guangdong',
        city: 'Shenzhen',
        organization: 'AS4134 Chinanet',
        timezone: 'Asia/Shanghai',
        accuracy: 10,
        latitude: '22.5455',
        longitude: '114.0683',
      }),
    })

    await fetchOne('121.35.47.43')

    expect(global.fetch).toHaveBeenCalledWith('https://get.geojs.io/v1/ip/geo/121.35.47.43.json')
    const entry = getEntry('121.35.47.43')
    expect(entry.status).toBe('success')
    expect(entry.label).toBe('CN · Guangdong · Shenzhen')
    expect(entry.detail?.organization).toBe('AS4134 Chinanet')
  })

  it('marks the entry as error when the response has no country_code', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ ip: '192.0.2.55', organization: 'AS64512 Unknown' }),
    })

    await fetchOne('192.0.2.55')

    expect(getEntry('192.0.2.55').status).toBe('error')
  })

  it('marks the entry as error when the request rejects', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockRejectedValue(new Error('network down'))

    await fetchOne('198.51.100.7')

    expect(getEntry('198.51.100.7').status).toBe('error')
  })

  it('does not re-fetch a cached successful IP unless forced', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ ip: '8.8.8.8', country_code: 'US', region: 'California', city: 'Mountain View' }),
    })

    await fetchOne('8.8.8.8')
    expect(global.fetch).toHaveBeenCalledTimes(1)

    await fetchOne('8.8.8.8')
    expect(global.fetch).toHaveBeenCalledTimes(1)

    await fetchOne('8.8.8.8', true)
    expect(global.fetch).toHaveBeenCalledTimes(2)
  })
})

describe('fetchBatch', () => {
  beforeEach(() => {
    localStorage.clear()
    global.fetch = vi.fn()
  })

  it('deduplicates IPs and skips private addresses without a network call', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => [{ ip: '203.0.113.10', country_code: 'US', region: 'Texas', city: 'Dallas' }],
    })

    await fetchBatch(['203.0.113.10', '203.0.113.10', '10.0.0.5'])

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const calledUrl = (global.fetch as any).mock.calls[0][0] as string
    expect(calledUrl).toContain('ip=203.0.113.10')
    expect(calledUrl).not.toContain('203.0.113.10,203.0.113.10')
    expect(getEntry('10.0.0.5').status).toBe('private')
    expect(getEntry('203.0.113.10').status).toBe('success')
  })

  it('returns false when a chunk request fails at the network level', async () => {
    const fetchMock = global.fetch as any
    fetchMock.mockRejectedValue(new Error('network down'))

    const ok = await fetchBatch(['203.0.113.50', '203.0.113.51'])

    expect(ok).toBe(false)
    expect(getEntry('203.0.113.50').status).toBe('error')
    expect(getEntry('203.0.113.51').status).toBe('error')
  })
})
