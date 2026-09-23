/** The label for "show this folder in the file manager", in the words the
 *  platform uses. `platform` is Wails' Environment().platform ("darwin",
 *  "windows", "linux"), or '' until it has been read. */
export function revealLabel(platform: string): string {
  if (platform === 'darwin') return 'Show in Finder'
  if (platform === 'windows') return 'Show in Explorer'
  return 'Open in File Manager'
}
