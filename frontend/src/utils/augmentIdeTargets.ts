export type AugmentQuickLoginEditorTarget =
  | 'vscode'
  | 'cursor'
  | 'kiro'
  | 'trae'
  | 'windsurf'
  | 'qodo'
  | 'codebuddy'
  | 'antigravity'

export type AugmentQuickLoginEditorTargetStatus = 'verified' | 'needsVerification' | 'requiresOverride'

export interface AugmentQuickLoginEditorTargetDescriptor {
  id: AugmentQuickLoginEditorTarget
  labelKey: string
  descriptionKey: string
  shortLabel: string
  schemeVerified: boolean
  handlerVerified: boolean
  enabledByDefault: boolean
  overrideRequired: boolean
  status: AugmentQuickLoginEditorTargetStatus
  statusBadgeKey: string
}

export const AUGMENT_QUICK_LOGIN_EDITOR_TARGET_STORAGE_KEY = 'augment.quickLogin.editorTarget'

export const AUGMENT_IDE_TARGETS: AugmentQuickLoginEditorTargetDescriptor[] = [
  {
    id: 'vscode',
    labelKey: 'plugin.augment.quickLogin.editors.vscode',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.vscode',
    shortLabel: 'VS',
    schemeVerified: true,
    handlerVerified: true,
    enabledByDefault: true,
    overrideRequired: false,
    status: 'verified',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.verified',
  },
  {
    id: 'cursor',
    labelKey: 'plugin.augment.quickLogin.editors.cursor',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.cursor',
    shortLabel: 'CU',
    schemeVerified: true,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'requiresOverride',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.requiresOverride',
  },
  {
    id: 'kiro',
    labelKey: 'plugin.augment.quickLogin.editors.kiro',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.kiro',
    shortLabel: 'KI',
    schemeVerified: false,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'needsVerification',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.needsVerification',
  },
  {
    id: 'trae',
    labelKey: 'plugin.augment.quickLogin.editors.trae',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.trae',
    shortLabel: 'TR',
    schemeVerified: true,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'requiresOverride',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.requiresOverride',
  },
  {
    id: 'windsurf',
    labelKey: 'plugin.augment.quickLogin.editors.windsurf',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.windsurf',
    shortLabel: 'WI',
    schemeVerified: false,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'needsVerification',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.needsVerification',
  },
  {
    id: 'qodo',
    labelKey: 'plugin.augment.quickLogin.editors.qodo',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.qodo',
    shortLabel: 'QO',
    schemeVerified: false,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'needsVerification',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.needsVerification',
  },
  {
    id: 'codebuddy',
    labelKey: 'plugin.augment.quickLogin.editors.codebuddy',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.codebuddy',
    shortLabel: 'CB',
    schemeVerified: false,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'needsVerification',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.needsVerification',
  },
  {
    id: 'antigravity',
    labelKey: 'plugin.augment.quickLogin.editors.antigravity',
    descriptionKey: 'plugin.augment.quickLogin.editorDescriptions.antigravity',
    shortLabel: 'AG',
    schemeVerified: false,
    handlerVerified: false,
    enabledByDefault: false,
    overrideRequired: true,
    status: 'needsVerification',
    statusBadgeKey: 'plugin.augment.quickLogin.editorStatus.needsVerification',
  },
]

const augmentIDEIDs = new Set<AugmentQuickLoginEditorTarget>(AUGMENT_IDE_TARGETS.map((target) => target.id))

export function isAugmentQuickLoginEditorTarget(
  value: string | null | undefined
): value is AugmentQuickLoginEditorTarget {
  return Boolean(value && augmentIDEIDs.has(value as AugmentQuickLoginEditorTarget))
}

export function getAugmentQuickLoginEditorTargetDescriptor(
  id: AugmentQuickLoginEditorTarget
): AugmentQuickLoginEditorTargetDescriptor {
  return AUGMENT_IDE_TARGETS.find((target) => target.id === id) ?? AUGMENT_IDE_TARGETS[0]
}
