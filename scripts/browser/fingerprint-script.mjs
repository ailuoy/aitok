// 保留真实 UA、UA-CH、GPU、CPU、内存和音频；只对图像读取与布局测量增加稳定微扰。
// 此兼容层不是浏览器内核级实现，不能保证网站无法识别脚本包装。
function installFingerprint(seed) {
  if (/^(?:chrome|chrome-extension|devtools):/.test(globalThis.location?.protocol || '')) return;
  const marker = Symbol.for('aitok.fingerprint.v1');
  if (globalThis[marker]) return;
  Object.defineProperty(globalThis, marker, { value: true });
  let key = 2166136261;
  for (const character of seed) key = Math.imul(key ^ character.charCodeAt(0), 16777619) >>> 0;
  const hash = (x, y) => {
    let value = Math.imul(key ^ x, 1597334677) ^ Math.imul(y, 3812015801);
    value = Math.imul(value ^ (value >>> 16), 2246822507);
    return (value ^ (value >>> 13)) >>> 0;
  };
  const noise = (pixels, width, height, x = 0, y = 0, offset = 0) => {
    if (!(pixels instanceof Uint8Array || pixels instanceof Uint8ClampedArray) || width <= 0 || height <= 0 || pixels.length - offset < width * height * 4) return;
    for (let row = 0; row < height; row++) for (let col = 0; col < width; col++) {
      const value = hash(x + col, y + row), index = offset + (row * width + col) * 4;
      if ((value & 31) === 0 && pixels[index + 3] !== 0) pixels[index + (value >>> 5) % 3] ^= 1;
    }
  };
  const wrap = (prototype, name, create) => {
    if (!prototype) return;
    const descriptor = Object.getOwnPropertyDescriptor(prototype, name);
    if (typeof descriptor?.value !== 'function') return;
    Object.defineProperty(prototype, name, { ...descriptor, value: create(descriptor.value) });
  };
  const originals = new Map();
  for (const Context of [globalThis.CanvasRenderingContext2D, globalThis.OffscreenCanvasRenderingContext2D]) {
    if (!Context) continue;
    originals.set(Context.prototype, Context.prototype.getImageData);
    wrap(Context.prototype, 'getImageData', original => function (...args) {
      const result = Reflect.apply(original, this, args);
      const x = Math.trunc(Number(args[0])), y = Math.trunc(Number(args[1]));
      noise(result.data, result.width, result.height, Number(args[2]) < 0 ? x - result.width : x, Number(args[3]) < 0 ? y - result.height : y);
      return result;
    });
  }
  const noisyCopy = canvas => {
    if (!canvas.width || !canvas.height) return canvas;
    const copy = typeof document === 'object' && canvas instanceof globalThis.HTMLCanvasElement ? document.createElement('canvas') : new OffscreenCanvas(canvas.width, canvas.height);
    copy.width = canvas.width; copy.height = canvas.height;
    const context = copy.getContext('2d');
    context.drawImage(canvas, 0, 0);
    const original = originals.get(Object.getPrototypeOf(context));
    const pixels = Reflect.apply(original, context, [0, 0, copy.width, copy.height]);
    noise(pixels.data, pixels.width, pixels.height);
    context.putImageData(pixels, 0, 0);
    return copy;
  };
  for (const name of ['toDataURL', 'toBlob']) {
    wrap(globalThis.HTMLCanvasElement?.prototype, name, original => function (...args) {
      return Reflect.apply(original, noisyCopy(this), args);
    });
  }
  wrap(globalThis.OffscreenCanvas?.prototype, 'convertToBlob', original => function (...args) {
    // convertToBlob 的错误必须继续以 Promise 拒绝的形式返回。
    try { return Reflect.apply(original, noisyCopy(this), args); }
    catch (error) { return Promise.reject(error); }
  });
  for (const Context of [globalThis.WebGLRenderingContext, globalThis.WebGL2RenderingContext]) {
    wrap(Context?.prototype, 'readPixels', original => function (...args) {
      const result = Reflect.apply(original, this, args);
      if (args[4] === 0x1908 && args[5] === 0x1401 && Number.isInteger(args[2]) && Number.isInteger(args[3])) noise(args[6], args[2], args[3], args[0], args[1], args[7] || 0);
      return result;
    });
  }
  const epsilon = ((key % 997) + 1) / 10000000;
  const rect = value => value.width || value.height ? new DOMRect(value.x + epsilon, value.y + epsilon, value.width, value.height) : value;
  for (const Type of [globalThis.Element, globalThis.Range]) {
    wrap(Type?.prototype, 'getBoundingClientRect', original => function (...args) { return rect(Reflect.apply(original, this, args)); });
    wrap(Type?.prototype, 'getClientRects', original => function (...args) {
      const list = Reflect.apply(original, this, args);
      return new Proxy(list, { get(target, property) {
        if (property === 'item') return index => { const value = target.item(index); return value && rect(value); };
        if (property === Symbol.iterator) return function* () { for (const value of target) yield rect(value); };
        const value = Reflect.get(target, property, target);
        return typeof property === 'string' && /^\d+$/.test(property) && value ? rect(value) : typeof value === 'function' ? value.bind(target) : value;
      } });
    });
  }
}

export const fingerprintSource = seed => `(${installFingerprint.toString()})(${JSON.stringify(seed)})`;
