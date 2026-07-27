import type { LoginAgreementDocument } from '@/types'

export function localizeLoginAgreementDocument(
  document: LoginAgreementDocument,
  locale: string,
): LoginAgreementDocument {
  if (locale.toLowerCase().startsWith('zh')) {
    return document
  }

  return {
    ...document,
    title: document.title_en?.trim() || document.title,
    content_md: document.content_md_en?.trim() || document.content_md,
  }
}
