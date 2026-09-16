import { build } from 'esbuild';
import { cp, mkdir } from 'node:fs/promises';

await mkdir(new URL('./build/', import.meta.url), { recursive: true });
await build({ entryPoints: ['src/main.mjs'], outfile: 'build/main.mjs', bundle: true, platform: 'node', format: 'esm', target: 'node22', external: ['electron'] });
await cp('src/preload.cjs', 'build/preload.cjs');
await cp('ui', 'build/ui', { recursive: true });
await cp('assets', 'build/assets', { recursive: true });
