import React, { useState } from 'react';
import { createPortal } from 'react-dom';
import Dialog from './Dialog';

export default function ImagePreview({ src, alt = '图片预览' }) {
  const [open, setOpen] = useState(false);
  return <><button type="button" className="image-thumbnail" aria-label={'放大' + alt} onClick={() => setOpen(true)}><img src={src} alt={alt} loading="lazy" /></button>
    {open && createPortal(<Dialog title={alt} className="image-preview-dialog" onClose={() => setOpen(false)}><img className="image-full" src={src} alt={alt} /></Dialog>, document.body)}
  </>;
}
