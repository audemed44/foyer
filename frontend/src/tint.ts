import { useEffect, useState } from "preact/hooks";

/**
 * Samples an icon's dominant colour so its card can glow in the app's own
 * brand colour. Results are cached per session; icons served without CORS
 * headers can't be read and simply get no tint.
 */
const cache = new Map<string, Promise<string | null>>();

function sample(src: string): Promise<string | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.decoding = "async";
    img.onload = () => {
      try {
        const size = 24;
        const canvas = document.createElement("canvas");
        canvas.width = canvas.height = size;
        const ctx = canvas.getContext("2d", { willReadFrequently: true });
        if (!ctx) return resolve(null);
        ctx.drawImage(img, 0, 0, size, size);
        resolve(dominant(ctx.getImageData(0, 0, size, size).data));
      } catch {
        resolve(null); // tainted canvas
      }
    };
    img.onerror = () => resolve(null);
    img.src = src;
  });
}

/** Weighted average of the saturated, visible pixels, as "r g b". */
export function dominant(data: Uint8ClampedArray): string | null {
  let r = 0;
  let g = 0;
  let b = 0;
  let weight = 0;
  for (let i = 0; i < data.length; i += 4) {
    const a = data[i + 3] / 255;
    if (a < 0.5) continue;
    const [pr, pg, pb] = [data[i], data[i + 1], data[i + 2]];
    const max = Math.max(pr, pg, pb);
    const min = Math.min(pr, pg, pb);
    const saturation = max === 0 ? 0 : (max - min) / max;
    // Greys and near-black/white say little about the brand.
    if (saturation < 0.25 || max < 40) continue;
    const w = saturation * a;
    r += pr * w;
    g += pg * w;
    b += pb * w;
    weight += w;
  }
  if (weight < 3) return null;
  return `${Math.round(r / weight)} ${Math.round(g / weight)} ${Math.round(b / weight)}`;
}

export function useTint(src: string | null): string | null {
  const [tint, setTint] = useState<string | null>(null);
  useEffect(() => {
    if (!src) return setTint(null);
    let alive = true;
    if (!cache.has(src)) cache.set(src, sample(src));
    cache.get(src)!.then((t) => alive && setTint(t));
    return () => {
      alive = false;
    };
  }, [src]);
  return tint;
}
