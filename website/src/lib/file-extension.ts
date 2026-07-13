export function fileExtension(filename: string) {
  const basename = filename.split('/').pop() || ''
  const index = basename.lastIndexOf('.')
  if (index < 0 || index === basename.length - 1) {
    return basename.toLowerCase()
  }
  return basename.slice(index + 1).toLowerCase()
}
