import { useSyncExternalStore } from 'react';
import { launcherPort } from './localBrowser';

function subscribe(callback) {
  window.addEventListener('aitok-launcher-change', callback);
  window.addEventListener('storage', callback);
  return () => { window.removeEventListener('aitok-launcher-change', callback); window.removeEventListener('storage', callback); };
}

export default function useLauncherPort() {
  return useSyncExternalStore(subscribe, launcherPort, () => 15683);
}
