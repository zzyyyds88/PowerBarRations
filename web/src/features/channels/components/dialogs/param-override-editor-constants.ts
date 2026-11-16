/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ParamOverrideCondition = {
  id: string
  path: string
  mode: string
  value_text: string
  invert: boolean
  pass_missing_key: boolean
}

export type ParamOverrideOperation = {
  id: string
  description: string
  path: string
  mode: string
  from: string
  to: string
  value_text: string
  keep_origin: boolean
  logic: string
  conditions: ParamOverrideCondition[]
}

export type ParamOverrideEditorDialogProps = {
  open: boolean
  value: string
  onOpenChange: (open: boolean) => void
  onSave: (value: string) => void
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

export const OPERATION_MODE_OPTIONS = [
  { label: 'Set Field', value: 'set' },
  { label: 'Delete Field', value: 'delete' },
  { label: 'Append to End', value: 'append' },
  { label: 'Prepend to Start', value: 'prepend' },
  { label: 'Copy Field', value: 'copy' },
  { label: 'Move Field', value: 'move' },
  { label: 'String Replace', value: 'replace' },
  { label: 'Regex Replace', value: 'regex_replace' },
  { label: 'Trim Prefix', value: 'trim_prefix' },
  { label: 'Trim Suffix', value: 'trim_suffix' },
  { label: 'Ensure Prefix', value: 'ensure_prefix' },
  { label: 'Ensure Suffix', value: 'ensure_suffix' },
  { label: 'Trim Space', value: 'trim_space' },
  { label: 'To Lowercase', value: 'to_lower' },
  { label: 'To Uppercase', value: 'to_upper' },
  { label: 'Return Custom Error', value: 'return_error' },
  { label: 'Prune Object Items', value: 'prune_objects' },
  { label: 'Pass Through Headers', value: 'pass_headers' },
  { label: 'Sync Fields', value: 'sync_fields' },
  { label: 'Set Request Header', value: 'set_header' },
  { label: 'Delete Request Header', value: 'delete_header' },
  { label: 'Copy Request Header', value: 'copy_header' },
  { label: 'Move Request Header', value: 'move_header' },
]

export const OPERATION_MODE_VALUES = new Set(
  OPERATION_MODE_OPTIONS.map((o) => o.value)
)

export const OPERATION_MODE_LABEL_MAP = OPERATION_MODE_OPTIONS.reduce<
  Record<string, string>
>((acc, item) => {
  acc[item.value] = item.label
  return acc
}, {})

export const CONDITION_MODE_OPTIONS = [
  { label: 'Exact Match', value: 'full' },
  { label: 'Prefix', value: 'prefix' },
  { label: 'Suffix', value: 'suffix' },
  { label: 'Contains', value: 'contains' },
  { label: 'Greater Than', value: 'gt' },
  { label: 'Greater Than or Equal', value: 'gte' },
  { label: 'Less Than', value: 'lt' },
  { label: 'Less Than or Equal', value: 'lte' },
]

export const CONDITION_MODE_VALUES = new Set(
  CONDITION_MODE_OPTIONS.map((o) => o.value)
)

export const MODE_META: Record<
  string,
  {
    path?: boolean
    pathOptional?: boolean
    value?: boolean
    from?: boolean
    to?: boolean
    keepOrigin?: boolean
    pathAlias?: boolean
  }
> = {
  delete: { path: true },
  set: { path: true, value: true, keepOrigin: true },
  append: { path: true, value: true, keepOrigin: true },
  prepend: { path: true, value: true, keepOrigin: true },
  copy: { from: true, to: true },
  move: { from: true, to: true },
  replace: { path: true, from: true, to: false },
  regex_replace: { path: true, from: true, to: false },
  trim_prefix: { path: true, value: true },
  trim_suffix: { path: true, value: true },
  ensure_prefix: { path: true, value: true },
  ensure_suffix: { path: true, value: true },
  trim_space: { path: true },
  to_lower: { path: true },
  to_upper: { path: true },
  return_error: { value: true },
  prune_objects: { pathOptional: true, value: true },
  pass_headers: { value: true, keepOrigin: true },
  sync_fields: { from: true, to: true },
  set_header: { path: true, value: true, keepOrigin: true },
  delete_header: { path: true },
  copy_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
  move_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
}

export const VALUE_REQUIRED_MODES = new Set([
  'trim_prefix',
  'trim_suffix',
  'ensure_prefix',
  'ensure_suffix',
  'set_header',
  'return_error',
  'prune_objects',
  'pass_headers',
])

export const FROM_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'replace',
  'regex_replace',
  'copy_header',
  'move_header',
  'sync_fields',
])

export const TO_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'copy_header',
  'move_header',
  'sync_fields',
])

export const MODE_DESCRIPTIONS: Record<string, string> = {
  set: 'Write value to the target field',
  delete: 'Remove the target field',
  append: 'Append value to array / string / object end',
  prepend: 'Prepend value to array / string / object start',
  copy: 'Copy source field to target field',
  move: 'Move source field to target field',
  replace: 'Do string replacement in the target field',
  regex_replace: 'Do regex replacement in the target field',
  trim_prefix: 'Remove string prefix',
  trim_suffix: 'Remove string suffix',
  ensure_prefix: 'Ensure the string has a specified prefix',
  ensure_suffix: 'Ensure the string has a specified suffix',
  trim_space: 'Trim leading/trailing whitespace',
  to_lower: 'Convert string to lowercase',
  to_upper: 'Convert string to uppercase',
  return_error: 'Return a custom error immediately',
  prune_objects: 'Prune object items by conditions',
  pass_headers: 'Pass specified request headers to upstream',
  sync_fields: 'Auto-fill when one field exists and another is missing',
  set_header:
    'Set runtime request header: override entire value, or manipulate comma-separated tokens',
  delete_header: 'Delete a runtime request header',
  copy_header: 'Copy a request header',
  move_header: 'Move a request header',
}

export const SYNC_TARGET_TYPE_OPTIONS = [
  { label: 'Request Body Field', value: 'json' },
  { label: 'Request Header Field', value: 'header' },
]

// Templates

export const LEGACY_TEMPLATE = { temperature: 0, max_tokens: 1000 }

export const OPERATION_TEMPLATE = {
  operations: [
    {
      description: 'Set default temperature for openai/* models.',
      path: 'temperature',
      mode: 'set',
      value: 0.7,
      conditions: [{ path: 'model', mode: 'prefix', value: 'openai/' }],
      logic: 'AND',
    },
  ],
}

const HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description: 'Pass through X-Request-Id header to upstream.',
      mode: 'pass_headers',
      value: ['X-Request-Id'],
      keep_origin: true,
    },
  ],
}

const GEMINI_IMAGE_4K_TEMPLATE = {
  operations: [
    {
      description:
        'Set imageSize to 4K when model contains gemini/image and ends with 4k.',
      mode: 'set',
      path: 'generationConfig.imageConfig.imageSize',
      value: '4K',
      conditions: [
        { path: 'original_model', mode: 'contains', value: 'gemini' },
        { path: 'original_model', mode: 'contains', value: 'image' },
        { path: 'original_model', mode: 'suffix', value: '4k' },
      ],
      logic: 'AND',
    },
  ],
}

// Keep in sync with upstream Codex request headers:
// https://github.com/openai/codex/commit/7c7b4861d88960f7e3bd5b7f30f8351be666dd84
// https://github.com/openai/codex/commit/14df0e8833aad0d6d78287954b61ffac67af936c
// https://github.com/openai/codex/commit/ebdd8795e924a8149b616e46ca2ed7848c207a4b
const CODEX_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'Originator',
  'Session_id',
  'Thread_id',
  'Session-Id',
  'Thread-Id',
  'X-Client-Request-Id',
  'User-Agent',
  'X-Codex-Beta-Features',
  'X-Codex-Turn-State',
  'X-Codex-Turn-Metadata',
  'X-Codex-Window-Id',
  'X-Codex-Parent-Thread-Id',
  // 'X-Codex-Installation-Id',
  'X-OpenAI-Subagent',
  'X-OpenAI-Memgen-Request',
  // 'X-OAI-Attestation',
  'X-ResponsesAPI-Include-Timing-Metrics',
  'X-OpenAI-Internal-Codex-Responses-Lite',
]

const CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
  'X-Stainless-Timeout',
  'User-Agent',
  'X-App',
  'Anthropic-Beta',
  'Anthropic-Dangerous-Direct-Browser-Access',
  'Anthropic-Version',
]

const buildPassHeadersTemplate = (headers: string[]) => ({
  operations: [
    { mode: 'pass_headers', value: [...headers], keep_origin: true },
  ],
})

const CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      mode: 'pass_headers',
      value: [...CODEX_CLI_HEADER_PASSTHROUGH_HEADERS],
      keep_origin: true,
    },
  ],
}
const CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS
)

const AWS_BEDROCK_ANTHROPIC_COMPAT_TEMPLATE = {
  operations: [
    {
      description:
        'Normalize anthropic-beta header tokens for Bedrock compatibility.',
      mode: 'set_header',
      path: 'anthropic-beta',
      value: {
        'advanced-tool-use-2025-11-20': 'tool-search-tool-2025-10-19',
        bash_20241022: null,
        bash_20250124: null,
        'code-execution-2025-08-25': null,
        'compact-2026-01-12': 'compact-2026-01-12',
        'computer-use-2025-01-24': 'computer-use-2025-01-24',
        'computer-use-2025-11-24': 'computer-use-2025-11-24',
        'context-1m-2025-08-07': 'context-1m-2025-08-07',
        'context-management-2025-06-27': 'context-management-2025-06-27',
        'effort-2025-11-24': null,
        'fast-mode-2026-02-01': null,
        'files-api-2025-04-14': null,
        'fine-grained-tool-streaming-2025-05-14': null,
        'interleaved-thinking-2025-05-14': 'interleaved-thinking-2025-05-14',
        'mcp-client-2025-11-20': null,
        'mcp-client-2025-04-04': null,
        'mcp-servers-2025-12-04': null,
        'output-128k-2025-02-19': null,
        'structured-output-2024-03-01': null,
        'prompt-caching-scope-2026-01-05': null,
        'skills-2025-10-02': null,
        'structured-outputs-2025-11-13': null,
        text_editor_20241022: null,
        text_editor_20250124: null,
        'token-efficient-tools-2025-02-19': null,
        'tool-search-tool-2025-10-19': 'tool-search-tool-2025-10-19',
        'web-fetch-2025-09-10': null,
        'web-search-2025-03-05': null,
        'oauth-2025-04-20': null,
      },
    },
    {
      description:
        'Remove all tools[*].custom.input_examples before upstream relay.',
      mode: 'delete',
      path: 'tools.*.custom.input_examples',
    },
  ],
}

type TemplatePresetConfig = {
  label: string
  kind: 'operations' | 'legacy'
  payload: Record<string, unknown>
}

export const TEMPLATE_PRESET_CONFIG: Record<string, TemplatePresetConfig> = {
  operations_default: {
    label: 'New Format Template',
    kind: 'operations',
    payload: OPERATION_TEMPLATE,
  },
  legacy_default: {
    label: 'Legacy Format Template',
    kind: 'legacy',
    payload: LEGACY_TEMPLATE,
  },
  pass_headers_auth: {
    label: 'Header Passthrough (X-Request-Id)',
    kind: 'operations',
    payload: HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_image_4k: {
    label: 'Gemini Image 4K',
    kind: 'operations',
    payload: GEMINI_IMAGE_4K_TEMPLATE,
  },
  claude_cli_headers_passthrough: {
    label: 'Claude CLI Header Passthrough',
    kind: 'operations',
    payload: CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  codex_cli_headers_passthrough: {
    label: 'Codex CLI Header Passthrough',
    kind: 'operations',
    payload: CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  aws_bedrock_anthropic_beta_override: {
    label: 'AWS Bedrock Claude Compat',
    kind: 'operations',
    payload: AWS_BEDROCK_ANTHROPIC_COMPAT_TEMPLATE,
  },
}
