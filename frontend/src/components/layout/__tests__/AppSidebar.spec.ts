import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar Augment Gateway admin navigation', () => {
  it('keeps the Augment Gateway admin link visible in simple mode', () => {
    expect(componentSource).toContain("{ path: '/admin/augment-gateway', label: t('admin.augmentGateway.title'), icon: GlobeIcon },")
    expect(componentSource).not.toContain("{ path: '/admin/augment-gateway', label: t('admin.augmentGateway.title'), icon: GlobeIcon, hideInSimpleMode: true },")
  })
})


describe('AppSidebar Codex entry center user navigation', () => {
  it('exposes /codex in the same user sidebar as Augment Quick Login', () => {
    expect(componentSource).toContain("{ path: '/plugin/augment/quick-login', label: t('plugin.augment.quickLogin.title'), icon: GlobeIcon },")
    expect(componentSource).toContain("{ path: '/codex', label: t('codex.title'), icon: ")
  })
})

describe('AppSidebar logo home navigation', () => {
  it('links both logo and site name to the computed home path', () => {
    expect(componentSource).toContain(':to="homePath"')
    expect(componentSource).toContain("const homePath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))")
    expect(componentSource.match(/@click="handleMenuItemClick\(homePath\)"/g)?.length).toBeGreaterThanOrEqual(2)
  })
})
