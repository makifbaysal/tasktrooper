import { contextBridge, ipcRenderer, type IpcRendererEvent } from "electron";
import { SHELL_BRIDGE_KEY, SHELL_CHANNELS, SHELL_EVENTS, type ShellBridge } from "../ipc/channels.js";
import type { AppInfo, CloudStatus, SupervisorSnapshot, UpdateStatus } from "../ipc/types.js";

/**
 * The native chrome's preload.
 *
 * It used to be a switchboard for a whole local UI. It is now a handful of
 * calls, because the local UI went away: the hosted web app is the product, and
 * the only thing left on this side of the window is a title bar with a status
 * pill, a reload button, the update affordance, and the screen that says so
 * honestly when the hosted app cannot be reached.
 *
 * Almost nothing here changes anything on this machine. Connect, the workspace
 * picker and the preflight moved to the hosted app's own Settings → Local
 * Runner page, over the cloud bridge, where every call is origin-checked.
 *
 * The exception is `restartToUpdate`, which drives the chrome's update popup.
 * The main process checks that the sender is this window before acting.
 * Settings has its own copy on the cloud bridge, behind that bridge's origin
 * check; both reach the same updater.
 */

function subscribe<T>(channel: string, cb: (payload: T) => void): () => void {
  const listener = (_event: IpcRendererEvent, payload: T): void => cb(payload);
  ipcRenderer.on(channel, listener);
  return () => {
    ipcRenderer.removeListener(channel, listener);
  };
}

const bridge: ShellBridge = {
  appInfo: () => ipcRenderer.invoke(SHELL_CHANNELS.appInfo) as Promise<AppInfo>,
  supervisorState: () => ipcRenderer.invoke(SHELL_CHANNELS.supervisorGet) as Promise<SupervisorSnapshot>,
  cloudStatus: () => ipcRenderer.invoke(SHELL_CHANNELS.cloudStatus) as Promise<CloudStatus>,
  reloadCloud: () => ipcRenderer.invoke(SHELL_CHANNELS.cloudReload) as Promise<void>,
  updateStatus: () => ipcRenderer.invoke(SHELL_CHANNELS.updateGet) as Promise<UpdateStatus>,
  checkForUpdate: () => ipcRenderer.invoke(SHELL_CHANNELS.updateCheck) as Promise<UpdateStatus>,
  restartToUpdate: () => ipcRenderer.invoke(SHELL_CHANNELS.updateRestart) as Promise<void>,
  onSupervisorState: (cb) => subscribe<SupervisorSnapshot>(SHELL_EVENTS.supervisorState, cb),
  onCloudStatus: (cb) => subscribe<CloudStatus>(SHELL_EVENTS.cloudStatus, cb),
  onUpdateStatus: (cb) => subscribe<UpdateStatus>(SHELL_EVENTS.updateStatus, cb),
  onFullScreen: (cb) => subscribe<boolean>(SHELL_EVENTS.fullScreen, cb),
};

contextBridge.exposeInMainWorld(SHELL_BRIDGE_KEY, bridge);
