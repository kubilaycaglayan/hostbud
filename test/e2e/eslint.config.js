import js from '@eslint/js'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['node_modules/**', 'results/**'] },
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    // Target profiles in playwright.config.ts (file names, @desktop/@phone/
    // @loopback tags), so the report's skipped count stays meaningful.
    files: ['tests/**/*.ts'],
    rules: {
      'no-restricted-syntax': ['error', {
        selector: "CallExpression[callee.object.name='test'][callee.property.name='skip']",
        message: 'Use a profile tag (@desktop, @phone, @loopback) or a file-name rule in playwright.config.ts instead of a runtime test.skip().',
      }],
    },
  },
)
