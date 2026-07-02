import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'build'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
  {
    files: ['src/views/**/*.tsx', 'src/layouts/**/*.tsx'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXElement[openingElement.name.name='button']",
          message: 'Use Button from src/components/ui/button instead of a native <button> in page and layout files.',
        },
        {
          selector: "JSXElement[openingElement.name.name='input']",
          message: 'Use Input from src/components/ui/input instead of a native <input> in page and layout files.',
        },
        {
          selector: "JSXElement[openingElement.name.name='select']",
          message: 'Use Select from src/components/ui/select instead of a native <select> in page and layout files.',
        },
      ],
    },
  },
)
