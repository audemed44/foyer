import {
  ArrowUpRight,
  Check,
  Copy,
  Download,
  Paperclip,
  Send,
  Trash2,
  Upload,
} from "lucide-preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { api, uploadDrop } from "../api";
import { usePoll, useNow } from "../hooks";
import { dayLabel, fileExt, formatBytes, targetAccepts, timeAgo } from "../lib";
import type { DropItem, DropResponse, DropTarget } from "../types";
import { Icon } from "./Icon";

/**
 * Drop: a shared inbox for notes, links and files. Anything saved here (or
 * shared to the installed app from a phone) shows up on every device.
 */
export function DropPage() {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll<DropResponse>(api.drop, 10000, [version]);
  const { data: targets } = usePoll<DropTarget[]>(api.dropTargets, 120000);
  const [removed, setRemoved] = useState<Set<string>>(new Set());
  const [notice, setNotice] = useState<{ tone: "good" | "bad"; text: string } | null>(() =>
    shareError(),
  );
  const refresh = () => setVersion((v) => v + 1);

  const items = (data?.items ?? []).filter((it) => !removed.has(it.id));
  const days = useMemo(() => {
    const out = new Map<string, DropItem[]>();
    for (const it of items) {
      const key = new Date(it.created).toDateString();
      if (!out.has(key)) out.set(key, []);
      out.get(key)!.push(it);
    }
    return [...out.entries()];
  }, [items]);

  const remove = async (id: string) => {
    setRemoved((s) => new Set(s).add(id));
    try {
      await api.deleteDrop(id);
    } catch (e) {
      setRemoved((s) => {
        const next = new Set(s);
        next.delete(id);
        return next;
      });
      setNotice({ tone: "bad", text: e instanceof Error ? e.message : String(e) });
    }
  };

  const files = items.filter((it) => it.kind === "file").length;
  return (
    <section class="drop-page">
      <header class="page-head">
        <p class="eyebrow eyebrow-accent">Inbox</p>
        <h1 class="page-title">Drop</h1>
        <p class="drop-lede">
          Notes, links and files, on every device. Share to the installed app from your phone to
          send things here.
        </p>
      </header>

      <Composer
        maxFile={data?.max_file ?? 0}
        onSaved={refresh}
        onError={(text) => setNotice({ tone: "bad", text })}
      />

      {notice && (
        <div class={`drop-notice ${notice.tone}`} role="status">
          <span>{notice.text}</span>
          <button class="btn btn-ghost" onClick={() => setNotice(null)}>
            Dismiss
          </button>
        </div>
      )}
      {error && !data && <div class="empty">{error}</div>}
      {!data && !error && <div class="widget-loading skeleton drop-skeleton" />}
      {data && items.length === 0 && (
        <div class="empty">Nothing here yet. Paste something above or drop a file anywhere.</div>
      )}

      {days.map(([key, list], i) => (
        <div class="drop-day" key={key}>
          <div class="group-head">
            <div class="group-title">
              <span class="group-index">{String(i + 1).padStart(2, "0")}</span>
              <h2 class="group-name">{dayLabel(new Date(list[0].created))}</h2>
            </div>
            <span class="spacer" />
            <span class="eyebrow">
              {list.length} {list.length === 1 ? "item" : "items"}
            </span>
          </div>
          <ul class="drop-list stagger">
            {list.map((it) => (
              <DropRow
                key={it.id}
                item={it}
                targets={targets ?? []}
                onDelete={() => remove(it.id)}
                onNotice={setNotice}
              />
            ))}
          </ul>
        </div>
      ))}
      {data && items.length > 0 && (
        <p class="eyebrow drop-usage">
          {items.length} items · {files} files · {formatBytes(data.usage)} stored
        </p>
      )}
    </section>
  );
}

/** An error passed back by the share target (#/drop?error=…), shown once. */
function shareError(): { tone: "bad"; text: string } | null {
  const query = window.location.hash.split("?")[1] ?? "";
  const error = new URLSearchParams(query).get("error");
  if (!error) return null;
  history.replaceState(null, "", "#/drop");
  return { tone: "bad", text: `Couldn't save what you shared: ${error}` };
}

function Composer(props: {
  maxFile: number;
  onSaved: () => void;
  onError: (message: string) => void;
}) {
  const [text, setText] = useState("");
  const [progress, setProgress] = useState<{ label: string; value: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const busy = progress !== null;

  const send = async (form: FormData, label: string) => {
    setProgress({ label, value: 0 });
    try {
      await uploadDrop(form, (value) => setProgress({ label, value }));
      props.onSaved();
      return true;
    } catch (e) {
      props.onError(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setProgress(null);
    }
  };

  const sendFiles = async (list: FileList | File[]) => {
    const files = [...list];
    if (!files.length || busy) return;
    const big = files.find((f) => props.maxFile && f.size > props.maxFile);
    if (big) {
      props.onError(`${big.name} is larger than the ${formatBytes(props.maxFile)} limit.`);
      return;
    }
    const form = new FormData();
    for (const f of files) form.append("files", f, f.name);
    const label = files.length === 1 ? files[0].name : `${files.length} files`;
    await send(form, label);
  };

  const sendText = async () => {
    if (!text.trim() || busy) return;
    const form = new FormData();
    form.append("text", text);
    if (await send(form, "note")) setText("");
  };

  // Files can be dropped anywhere on the page.
  useEffect(() => {
    let depth = 0;
    const hasFiles = (e: DragEvent) => e.dataTransfer?.types.includes("Files") ?? false;
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth++;
      setDragging(true);
    };
    const leave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDragging(false);
    };
    const over = (e: DragEvent) => hasFiles(e) && e.preventDefault();
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      setDragging(false);
      if (e.dataTransfer?.files.length) sendFiles(e.dataTransfer.files);
    };
    document.addEventListener("dragenter", enter);
    document.addEventListener("dragleave", leave);
    document.addEventListener("dragover", over);
    document.addEventListener("drop", drop);
    return () => {
      document.removeEventListener("dragenter", enter);
      document.removeEventListener("dragleave", leave);
      document.removeEventListener("dragover", over);
      document.removeEventListener("drop", drop);
    };
  });

  return (
    <div class={`drop-composer ${dragging ? "dragging" : ""}`}>
      <textarea
        class="drop-input"
        value={text}
        rows={3}
        placeholder="Type or paste a note or a link…"
        disabled={busy}
        onInput={(e) => setText(e.currentTarget.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            sendText();
          }
        }}
        onPaste={(e) => {
          const files = e.clipboardData?.files;
          if (files?.length) {
            e.preventDefault();
            sendFiles(files);
          }
        }}
      />
      <div class="drop-bar">
        <input
          ref={input}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            const files = e.currentTarget.files;
            if (files) sendFiles(files);
            e.currentTarget.value = "";
          }}
        />
        <button class="btn btn-ghost" onClick={() => input.current?.click()} disabled={busy}>
          <Paperclip size={14} /> Attach files
        </button>
        {progress ? (
          <span class="drop-progress">
            <span class="eyebrow">
              Saving {progress.label} · {Math.round(progress.value * 100)}%
            </span>
            <span class="bar">
              <span style={{ width: `${progress.value * 100}%` }} />
            </span>
          </span>
        ) : (
          <span class="drop-hint">
            {props.maxFile ? `Files up to ${formatBytes(props.maxFile)} · ` : ""}Ctrl+Enter to save
          </span>
        )}
        <span class="spacer" />
        <button class="btn btn-primary" onClick={sendText} disabled={busy || !text.trim()}>
          Save
        </button>
      </div>
      {dragging && (
        <div class="drop-overlay" aria-hidden="true">
          <Upload size={28} />
          <span>Drop to save</span>
        </div>
      )}
    </div>
  );
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // No clipboard API (plain HTTP); fall back to a selection copy.
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

function DropRow(props: {
  item: DropItem;
  targets: DropTarget[];
  onDelete: () => void;
  onNotice: (n: { tone: "good" | "bad"; text: string }) => void;
}) {
  const { item } = props;
  const now = useNow();
  const [copied, setCopied] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [sending, setSending] = useState<string | null>(null);

  const copy = async (text: string) => {
    if (await copyText(text)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    }
  };
  const sendTo = async (target: DropTarget) => {
    setSending(target.service);
    try {
      const res = await api.sendDrop(item.id, target.service);
      props.onNotice({ tone: "good", text: `${item.file?.name}: ${res.message}` });
    } catch (e) {
      props.onNotice({ tone: "bad", text: e instanceof Error ? e.message : String(e) });
    } finally {
      setSending(null);
    }
  };

  const when = timeAgo(new Date(item.created), now);
  const copyButton = (text: string) => (
    <button class="icon-btn" title="Copy" aria-label="Copy" onClick={() => copy(text)}>
      {copied ? <Check size={15} /> : <Copy size={15} />}
    </button>
  );
  const deleteButton = (
    <button class="icon-btn" title="Delete" aria-label="Delete" onClick={props.onDelete}>
      <Trash2 size={15} />
    </button>
  );

  if (item.kind === "link") {
    let host = item.url!;
    try {
      host = new URL(item.url!).host.replace(/^www\./, "");
    } catch {
      // keep the raw URL
    }
    return (
      <li class="drop-row">
        <span class="eyebrow drop-kind">Link · {when}</span>
        <a class="drop-link" href={item.url} target="_blank" rel="noopener noreferrer">
          <span class="drop-title">{item.title || host}</span>
          <span class="drop-sub">
            {host}
            <ArrowUpRight size={12} />
          </span>
        </a>
        <span class="drop-actions">
          {copyButton(item.url!)}
          {deleteButton}
        </span>
      </li>
    );
  }

  if (item.kind === "file" && item.file) {
    const f = item.file;
    const image = /^image\/(png|jpeg|gif|webp|avif)$/.test(f.type);
    const matching = props.targets.filter((t) => targetAccepts(t, f.name, f.type));
    return (
      <li class={`drop-row ${matching.length ? "has-send" : ""}`}>
        <span class="eyebrow drop-kind">
          {fileExt(f.name) || "File"} · {when}
        </span>
        <a
          class="drop-file"
          href={api.dropFileUrl(item.id)}
          target="_blank"
          rel="noopener noreferrer"
        >
          <span class="drop-thumb">
            {image ? (
              <img src={api.dropFileUrl(item.id)} alt="" loading="lazy" decoding="async" />
            ) : (
              <span>{fileExt(f.name) || "FILE"}</span>
            )}
          </span>
          <span class="drop-file-text">
            <span class="drop-title">{f.name}</span>
            <span class="drop-sub">{formatBytes(f.size)}</span>
          </span>
        </a>
        <span class="drop-actions">
          {matching.map((t) => (
            <button
              key={t.service}
              class="btn drop-send"
              onClick={() => sendTo(t)}
              disabled={sending !== null}
              title={`Upload to ${t.name}`}
            >
              {t.icon ? <Icon icon={t.icon} name={t.name} size={14} /> : <Send size={14} />}
              {sending === t.service ? "Sending…" : t.label}
            </button>
          ))}
          <a
            class="icon-btn"
            href={api.dropFileUrl(item.id, true)}
            download={f.name}
            title="Download"
            aria-label="Download"
          >
            <Download size={15} />
          </a>
          {deleteButton}
        </span>
      </li>
    );
  }

  const text = item.text ?? "";
  const long = text.length > 480 || text.split("\n").length > 8;
  return (
    <li class="drop-row">
      <span class="eyebrow drop-kind">Note · {when}</span>
      <div class="drop-note">
        {item.title && <span class="drop-title">{item.title}</span>}
        <p class={`drop-text ${long && !expanded ? "clamped" : ""}`}>{text}</p>
        {long && (
          <button class="drop-more" onClick={() => setExpanded(!expanded)}>
            {expanded ? "Show less" : "Show more"}
          </button>
        )}
      </div>
      <span class="drop-actions">
        {copyButton(text)}
        {deleteButton}
      </span>
    </li>
  );
}
