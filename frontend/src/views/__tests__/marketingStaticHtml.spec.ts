import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { JSDOM } from 'jsdom'
import { describe, expect, it } from 'vitest'

function renderMarketingPage(filename: string, authUser?: Record<string, unknown>) {
  const html = readFileSync(resolve(__dirname, '../../../public/brand/mockups', filename), 'utf8')
  const dom = new JSDOM(html, {
    runScripts: 'dangerously',
    url: 'https://www.5566676.xyz/',
    beforeParse(window) {
      if (authUser) {
        window.localStorage.setItem('auth_token', 'token-value')
        window.localStorage.setItem('auth_user', JSON.stringify(authUser))
      }
    },
  })

  const link = dom.window.document.querySelector<HTMLAnchorElement>('[data-auth-cta="login"]')
  return { dom, link }
}

describe('marketing static auth CTA', () => {
  it('keeps the header CTA as login when no persisted session exists', () => {
    const { link } = renderMarketingPage('homepage-codex-premium-v6.html')

    expect(link?.textContent?.trim()).toBe('登录')
    expect(link?.getAttribute('href')).toBe('/login')
  })

  it('changes the header CTA to dashboard when a persisted user session exists', () => {
    const { link } = renderMarketingPage('homepage-codex-premium-v6.html', { email: 'user@example.com' })

    expect(link?.textContent?.trim()).toBe('进入控制台')
    expect(link?.getAttribute('href')).toBe('/dashboard')
  })

  it('routes admin sessions to the admin dashboard', () => {
    const { link } = renderMarketingPage('codex-gateway.html', { email: 'admin@example.com', role: 'admin' })

    expect(link?.textContent?.trim()).toBe('进入控制台')
    expect(link?.getAttribute('href')).toBe('/admin/dashboard')
  })
})
