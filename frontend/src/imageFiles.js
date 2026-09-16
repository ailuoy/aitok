export const imagePattern = /^data:image\/(png|jpeg|gif);base64,[A-Za-z0-9+/]+={0,2}$/;

// 按内容识别 JPG，兼容截图的空 MIME；统一限制文件大小和解码尺寸。
export async function readImageFile(file) {
  if (file.size > 2 * 1024 * 1024) throw new Error('每张图片不能超过 2 MB，请缩小后添加');
  const bytes = new Uint8Array(await file.slice(0, 8).arrayBuffer());
  let mime;
  if (bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) mime = 'image/jpeg';
  else if ([137, 80, 78, 71, 13, 10, 26, 10].every((value, index) => bytes[index] === value)) mime = 'image/png';
  else if (['GIF87a', 'GIF89a'].includes(String.fromCharCode(...bytes.slice(0, 6)))) mime = 'image/gif';
  else throw new Error('支持 JPG、JPEG、PNG、GIF 图片，请检查文件格式');
  return new Promise((resolve, reject) => {
    const reader = new FileReader(); reader.onerror = () => reject(new Error('读取图片失败'));
    reader.onload = () => {
      const image = new Image(); image.onerror = () => reject(new Error('图片内容无效'));
      image.onload = () => image.naturalWidth * image.naturalHeight > 20000000 ? reject(new Error('图片分辨率过大，请缩小后添加')) : resolve(reader.result);
      image.src = reader.result;
    };
    reader.readAsDataURL(file.slice(0, file.size, mime));
  });
}
