import { build } from 'esbuild';
import { cp, mkdir } from 'node:fs/promises';
import { profiles } from './src/profiles.mjs';

const channel = process.argv.find(value => value.startsWith('--channel='))?.split('=')[1] || 'test';
if (!profiles[channel]) throw new Error('打包环境必须是 test 或 production');

await mkdir(new URL('./build/', import.meta.url), { recursive: true });
await build({ entryPoints: ['src/main.mjs'], outfile: 'build/main.mjs', bundle: true, platform: 'node', format: 'esm', target: 'node22', external: ['electron'], define: { __AITOK_CHANNEL__: JSON.stringify(channel) } });
await cp('src/preload.cjs', 'build/preload.cjs');
await cp('ui', 'build/ui', { recursive: true });
await cp('assets', 'build/assets', { recursive: true });
