import { describe, expect, it } from 'vitest'

import { localizeLoginAgreementDocument } from '@/utils/legalDocument'

const bilingualDocument = {
  id: 'terms',
  title: '服务条款',
  content_md: '中文内容',
  title_en: 'Terms of Service',
  content_md_en: 'English content',
}

describe('localizeLoginAgreementDocument', () => {
  it('uses Chinese fields for Chinese locales', () => {
    expect(localizeLoginAgreementDocument(bilingualDocument, 'zh-CN')).toMatchObject({
      title: '服务条款',
      content_md: '中文内容',
    })
  })

  it('uses English fields for English locales', () => {
    expect(localizeLoginAgreementDocument(bilingualDocument, 'en')).toMatchObject({
      title: 'Terms of Service',
      content_md: 'English content',
    })
  })

  it('falls back to the default fields when English content is missing', () => {
    expect(
      localizeLoginAgreementDocument(
        {
          id: 'legacy',
          title: '旧标题',
          content_md: '旧内容',
        },
        'en-US',
      ),
    ).toMatchObject({
      title: '旧标题',
      content_md: '旧内容',
    })
  })
})
