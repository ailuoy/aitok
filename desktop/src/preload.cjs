const { contextBridge, ipcRenderer } = require('electron');

// 只暴露固定操作，不向网页开放 Node、文件系统或任意 IPC。
const invoke = async (action, value) => {
  const result = await ipcRenderer.invoke('aitok:assistant', action, value);
  if (result.error) throw new Error(result.error);
  return result.data;
};
contextBridge.exposeInMainWorld('assistant', {
  state: () => invoke('state'),
  save: value => invoke('save', value),
  toggle: (id, enabled) => invoke('toggle', { id, enabled }),
  remove: id => invoke('remove', id),
  openSite: id => invoke('open-site', id),
  loginAtStartup: enabled => invoke('login-at-startup', enabled),
});
