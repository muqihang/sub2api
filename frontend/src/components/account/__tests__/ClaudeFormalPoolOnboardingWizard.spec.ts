import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { FormalPoolSession } from '@/api/admin/claudeOnboarding'
import ClaudeFormalPoolOnboardingWizard from '../ClaudeFormalPoolOnboardingWizard.vue'

const onboardingApi = vi.hoisted(() => ({
	createSession: vi.fn(),
	getSession: vi.fn(),
	testProxy: vi.fn(),
	attestBrowserEgress: vi.fn(),
	generateAuthUrl: vi.fn(),
	exchangeCodeAndCreate: vi.fn(),
	setupTokenCookieAuthAndCreate: vi.fn(),
	refreshOnly: vi.fn(),
	runtimeRegister: vi.fn(),
	healthcheck: vi.fn(),
	startWarming: vi.fn(),
	promoteProduction: vi.fn(),
}))

vi.mock('@/api/admin/claudeOnboarding', () => ({
	default: onboardingApi,
	...onboardingApi,
}))

function sessionFixture(overrides: Partial<FormalPoolSession> = {}): FormalPoolSession {
	return {
		id: 'legacy-session-1',
		version: 1,
		status: 'draft',
		pool_profile: 'normal',
		group_id: 9,
		account_name: 'legacy-test',
		concurrency: 1,
		browser_egress_check_status: 'idle',
		browser_egress_verified: false,
		...overrides,
	}
}

async function fillStartForm(wrapper: ReturnType<typeof mount>) {
	const numberInputs = wrapper.findAll('input[type="number"]')
	await numberInputs[0].setValue(7)
	await numberInputs[1].setValue(9)
	await wrapper.find('input[placeholder="例如：claude-oauth-01"]').setValue('legacy-test')
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
	const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
	if (!button) throw new Error(`button not found: ${text}`)
	return button
}

describe('ClaudeFormalPoolOnboardingWizard mutation retries', () => {
	beforeEach(() => {
		Object.values(onboardingApi).forEach((mock) => mock.mockReset())
		onboardingApi.getSession.mockResolvedValue(sessionFixture())
	})

	it('reuses a create operation key after an ambiguous failure', async () => {
		const wrapper = mount(ClaudeFormalPoolOnboardingWizard)
		await fillStartForm(wrapper)
		onboardingApi.createSession.mockRejectedValueOnce(new Error('network unavailable')).mockResolvedValueOnce(sessionFixture())

		await buttonByText(wrapper, '创建 onboarding session').trigger('click')
		await flushPromises()
		await buttonByText(wrapper, '创建 onboarding session').trigger('click')
		await flushPromises()

		expect(onboardingApi.createSession.mock.calls[1][1]).toBe(onboardingApi.createSession.mock.calls[0][1])
	})

	it('rotates a create operation key after a definitive failure', async () => {
		const wrapper = mount(ClaudeFormalPoolOnboardingWizard)
		await fillStartForm(wrapper)
		onboardingApi.createSession.mockRejectedValueOnce({ response: { status: 400, data: { message: 'invalid request' } } }).mockResolvedValueOnce(sessionFixture())

		await buttonByText(wrapper, '创建 onboarding session').trigger('click')
		await flushPromises()
		await buttonByText(wrapper, '创建 onboarding session').trigger('click')
		await flushPromises()

		expect(onboardingApi.createSession.mock.calls[1][1]).not.toBe(onboardingApi.createSession.mock.calls[0][1])
	})

	it('refetches on 409 and rejects a stale reconciliation snapshot', async () => {
		const wrapper = mount(ClaudeFormalPoolOnboardingWizard)
		await fillStartForm(wrapper)
		onboardingApi.createSession.mockResolvedValueOnce(sessionFixture({ version: 5 }))
		await buttonByText(wrapper, '创建 onboarding session').trigger('click')
		await flushPromises()
		onboardingApi.getSession.mockResolvedValueOnce(sessionFixture({ version: 4, status: 'stale' }))
		onboardingApi.testProxy.mockRejectedValueOnce({ response: { status: 409, data: { message: 'conflict' } } })

		await buttonByText(wrapper, '测试代理').trigger('click')
		await flushPromises()
		expect(onboardingApi.getSession).toHaveBeenCalledWith('legacy-session-1')

		onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({ version: 6, status: 'proxy_verified' }))
		await buttonByText(wrapper, '测试代理').trigger('click')
		await flushPromises()
		expect(onboardingApi.testProxy.mock.calls[0][0].version).toBe(5)
		expect(onboardingApi.testProxy.mock.calls[1][0].version).toBe(5)
	})
})
