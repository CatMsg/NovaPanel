export const isPageVisible = () => {
  if (typeof document === 'undefined') {
    return true
  }
  return document.visibilityState !== 'hidden'
}

export const onPageVisibilityChange = (callback: (visible: boolean) => void) => {
  if (typeof document === 'undefined') {
    return () => {}
  }

  const handler = () => callback(isPageVisible())
  document.addEventListener('visibilitychange', handler)

  return () => {
    document.removeEventListener('visibilitychange', handler)
  }
}
