import { useEffect, useState } from "preact/hooks";
import { iconUrl, monogram } from "../lib";

export function Icon({ icon, name, size = 28 }: { icon?: string; name: string; size?: number }) {
  const src = iconUrl(icon);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);

  if (!src || failed) {
    return (
      <span class="icon icon-mono" style={{ width: size, height: size, fontSize: size * 0.4 }}>
        {monogram(name)}
      </span>
    );
  }
  return (
    <img
      class="icon"
      src={src}
      alt=""
      width={size}
      height={size}
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
    />
  );
}
