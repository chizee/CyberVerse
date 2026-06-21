export type LaunchWorkspaceMode = 'offline' | 'live'

const LAUNCH_MODE_KEY = 'cyberverse.launchWorkspaceMode.v1'

export function loadLaunchWorkspaceMode(): LaunchWorkspaceMode {
  if (typeof window === 'undefined') return 'offline'
  const value = window.localStorage.getItem(LAUNCH_MODE_KEY)
  return value === 'live' ? 'live' : 'offline'
}

export function saveLaunchWorkspaceMode(mode: LaunchWorkspaceMode) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(LAUNCH_MODE_KEY, mode)
}
