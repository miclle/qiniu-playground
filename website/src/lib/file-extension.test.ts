import { expect, test } from 'vitest'

import { fileExtension } from './file-extension'

test('ignores dots in parent directories when a file has no extension', () => {
  expect(fileExtension('/workspace/my.project/README')).toBe('readme')
})
