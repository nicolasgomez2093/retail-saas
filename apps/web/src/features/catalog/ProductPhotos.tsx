import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { api, type Product, type ProductPhoto } from '../../shared/api/client';

const maxPhotos = 6;
const maxSourceBytes = 12 * 1024 * 1024;
const maxEdge = 1600;

async function preparePhoto(file: File): Promise<Blob> {
  if (file.size > maxSourceBytes) throw new Error('Seleccioná una imagen de hasta 12 MiB.');
  if (file.type !== 'image/jpeg' && file.type !== 'image/png') throw new Error('Seleccioná una imagen JPEG o PNG.');
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' }).catch(() => {
    throw new Error('No pudimos abrir esa imagen.');
  });
  try {
    const scale = Math.min(1, maxEdge / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext('2d', { alpha: false });
    if (!context) throw new Error('No pudimos preparar la imagen.');
    context.fillStyle = '#ffffff';
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((image) => image ? resolve(image) : reject(new Error('No pudimos preparar la imagen.')), 'image/jpeg', 0.93);
    });
  } finally {
    bitmap.close();
  }
}

function PhotoImage({ product, photo, alt }: { product: Product; photo: ProductPhoto; alt: string }) {
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [attempt, setAttempt] = useState(0);
  return <>
    {state !== 'error' && <img key={attempt} src={api.productPhotoURL(product.id, photo.id)} alt={alt}
      width={photo.width} height={photo.height} onLoad={() => setState('ready')} onError={() => setState('error')} />}
    {state === 'loading' && <p role="status">Cargando foto…</p>}
    {state === 'error' && <div>
      <p role="alert">No pudimos mostrar la foto.</p>
      <button type="button" className="button-secondary" onClick={() => {
        setState('loading');
        setAttempt((current) => current + 1);
      }}>Reintentar foto</button>
    </div>}
  </>;
}

function PhotoDialog({ product, photo, alt, returnTo, onClose }: {
  product: Product; photo: ProductPhoto; alt: string; returnTo: HTMLButtonElement | null; onClose: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    close.current?.focus();
    return () => {
      element?.close();
      if (returnTo?.isConnected && returnTo.getClientRects().length) returnTo.focus();
    };
  }, [returnTo]);

  return <dialog ref={dialog} className="catalog-photo-dialog" aria-labelledby={id} onCancel={(event) => {
    event.preventDefault();
    onClose();
  }}>
    <header>
      <h3 id={id}>{alt}</h3>
      <button ref={close} type="button" className="button-secondary" onClick={onClose}>Cerrar foto</button>
    </header>
    <PhotoImage product={product} photo={photo} alt={alt} />
  </dialog>;
}

export function ProductPhotos({ product, images, canManage, active, onChanged }: {
  product: Product;
  images: ProductPhoto[];
  canManage: boolean;
  active: boolean;
  onChanged: (images: ProductPhoto[]) => void;
}) {
  const [selectedId, setSelectedId] = useState('');
  const [expandedId, setExpandedId] = useState('');
  const opener = useRef<HTMLButtonElement>(null);
  const removeButton = useRef<HTMLButtonElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const photoHelp = useId();
  const selected = images.find((image) => image.id === selectedId) ?? images.find((image) => image.primary) ?? images[0];
  const expanded = images.find((image) => image.id === expandedId);
  const description = (photo: ProductPhoto) => 'Foto ' + (images.indexOf(photo) + 1) + ' de ' + product.name;
  useEffect(() => { if (!active) setExpandedId(''); }, [active]);
  const [busy, setBusy] = useState(false);
  const [pendingRemoval, setPendingRemoval] = useState('');
  useEffect(() => { if (active && pendingRemoval) confirmButton.current?.focus(); }, [active, pendingRemoval]);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  async function perform(change: () => Promise<{ images: ProductPhoto[] }>, message: string) {
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await change();
      onChanged(result.images);
      setPendingRemoval('');
      setNotice(message);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos actualizar las fotos.');
    } finally {
      setBusy(false);
    }
  }

  function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const file = new FormData(form).get('photo');
    if (!(file instanceof File) || !file.size) {
      setError('Seleccioná una foto.');
      return;
    }
    void perform(async () => {
      const prepared = await preparePhoto(file);
      const result = await api.uploadProductPhoto(product.id, prepared);
      form.reset();
      return result;
    }, 'Foto agregada.');
  }

  return (
    <section className="catalog-photos" aria-label="Fotos del producto">
      <h3>Fotos</h3>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {selected ? <div className="photo-gallery">
        <div className="catalog-photo-stage">
          <PhotoImage key={selected.id} product={product} photo={selected} alt={description(selected)} />
        </div>
        <div className="catalog-photo-controls">
          <p>{selected.primary ? 'Foto principal' : 'Foto adicional'}</p>
          {images.length > 1 && <ul className="catalog-photo-thumbnails" aria-label="Miniaturas">
            {images.map((photo, index) => <li key={photo.id}>
              <button type="button" className="button-secondary" aria-pressed={selected.id === photo.id}
                aria-label={'Ver foto ' + (index + 1) + (photo.primary ? ', principal' : '')} onClick={() => {
                  setSelectedId(photo.id);
                  setPendingRemoval('');
                }}>
                <img src={api.productPhotoURL(product.id, photo.id)} alt="" loading="lazy" />
              </button>
            </li>)}
          </ul>}
          <button type="button" className="button-secondary" aria-haspopup="dialog" onClick={(event) => {
            opener.current = event.currentTarget;
            setExpandedId(selected.id);
          }}>Ampliar foto</button>
          {canManage && <div className="actions">
            {pendingRemoval === selected.id ? <>
              <button ref={confirmButton} type="button" disabled={busy} onClick={() => void perform(
                () => api.deleteProductPhoto(product.id, selected.id), 'Foto quitada.',
              )}>Confirmar quitar</button>
              <button type="button" className="button-secondary" disabled={busy} onClick={() => {
                setPendingRemoval('');
                requestAnimationFrame(() => {
                  if (document.activeElement === document.body) removeButton.current?.focus();
                });
              }}>Cancelar</button>
            </> : <>
              {!selected.primary && <button type="button" className="button-secondary" disabled={busy} onClick={() => void perform(
                () => api.markProductPhotoPrimary(product.id, selected.id), 'Foto principal actualizada.',
              )}>Hacer principal</button>}
              <button type="button" ref={removeButton} className="button-secondary" disabled={busy}
                onClick={() => setPendingRemoval(selected.id)}>Quitar foto</button>
            </>}
          </div>}
        </div>
      </div> : <p>Todavía no hay fotos de este producto.</p>}
      {active && expanded && <PhotoDialog product={product} photo={expanded} alt={description(expanded)}
        returnTo={opener.current} onClose={() => setExpandedId('')} />}
      {canManage && (images.length < maxPhotos ? (
        <form aria-label="Subir foto de producto" onSubmit={upload}>
          <label>Foto JPEG o PNG<input name="photo" type="file" accept="image/jpeg,image/png" aria-describedby={photoHelp} required disabled={busy} /></label>
          <small id={photoHelp}>Hasta 12 MiB por archivo.</small>
          <button disabled={busy}>{busy ? 'Procesando…' : 'Subir foto'}</button>
        </form>
      ) : <p>Este producto ya tiene {maxPhotos} fotos.</p>)}
    </section>
  );
}
