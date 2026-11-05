import { ref } from "vue";
import { t } from "../i18n";

// Upload pipeline for the static-site file drawer: flatten drag-and-drop
// entries and folder picks into relative paths, detect name clashes, then
// stream the batch through XHR so progress reflects the whole request.
// The composable holds no component state: the shared busy/error/note flags and
// the current path/entries are injected, so the drawer stays the owner.

export type UploadItem = { file: File; rel: string };
export type UploadResult = {
  ok: boolean;
  status: number;
  error?: string;
  stored?: number;
  extracted?: number;
  skipped?: number;
};

export type FileUploadOptions = {
  api: (query?: string) => string;
  getCsrf: () => string;
  getPath: () => string;
  getEntryPaths: () => string[];
  reload: () => Promise<void>;
  setBusy: (value: boolean) => void;
  setError: (value: string) => void;
  setNote: (value: string) => void;
  resetInputs: () => void;
};

export function useFileUpload(options: FileUploadOptions) {
  const uploadProgress = ref<number | null>(null);
  const conflictPrompt = ref<{
    items: UploadItem[];
    conflicts: string[];
  } | null>(null);

  // A folder upload reports paths like "dist/assets/app.js"; drop the top-level
  // folder name so the contents land in the current directory, not a new one.
  function relPathFor(file: File): string {
    const rp = (file as File & { webkitRelativePath?: string })
      .webkitRelativePath;
    if (rp) {
      const parts = rp.split("/").filter(Boolean);
      if (parts.length > 1) return parts.slice(1).join("/");
    }
    return file.name;
  }

  function readAllEntries(
    reader: FileSystemDirectoryReader,
  ): Promise<FileSystemEntry[]> {
    return new Promise((resolve, reject) => {
      const out: FileSystemEntry[] = [];
      const next = () =>
        reader.readEntries((batch) => {
          if (!batch.length) return resolve(out);
          out.push(...batch);
          next();
        }, reject);
      next();
    });
  }

  async function walkEntry(
    entry: FileSystemEntry,
    base: string,
    out: UploadItem[],
    isTop: boolean,
  ): Promise<void> {
    if (entry.isFile) {
      const file = await new Promise<File>((resolve, reject) =>
        (entry as FileSystemFileEntry).file(resolve, reject),
      );
      out.push({ file, rel: base ? `${base}/${entry.name}` : entry.name });
      return;
    }
    if (entry.isDirectory) {
      const reader = (entry as FileSystemDirectoryEntry).createReader();
      const children = await readAllEntries(reader);
      const childBase = isTop
        ? base
        : base
          ? `${base}/${entry.name}`
          : entry.name;
      for (const child of children) await walkEntry(child, childBase, out, false);
    }
  }

  async function collectFromItems(
    items: DataTransferItemList,
  ): Promise<UploadItem[]> {
    const out: UploadItem[] = [];
    for (const item of Array.from(items)) {
      const getEntry = (item as DataTransferItem & {
        webkitGetAsEntry?: () => FileSystemEntry | null;
      }).webkitGetAsEntry;
      const entry = getEntry?.();
      if (entry) {
        await walkEntry(entry, "", out, true);
      } else {
        const file = item.getAsFile();
        if (file) out.push({ file, rel: file.name });
      }
    }
    return out;
  }

  function conflictsFor(items: UploadItem[]): string[] {
    const existing = new Set(options.getEntryPaths());
    return items.map((i) => i.rel).filter((rel) => existing.has(rel));
  }

  function beginUpload(items: UploadItem[]) {
    if (!items.length) return;
    const conflicts = conflictsFor(items);
    if (conflicts.length) {
      conflictPrompt.value = { items, conflicts };
      return;
    }
    void doUpload(items, true);
  }

  function cancelConflict() {
    conflictPrompt.value = null;
  }
  function confirmConflict(overwrite: boolean) {
    const items = conflictPrompt.value?.items || [];
    conflictPrompt.value = null;
    if (items.length) void doUpload(items, overwrite);
  }

  async function doUpload(items: UploadItem[], overwrite: boolean) {
    options.setBusy(true);
    options.setError("");
    options.setNote("");
    uploadProgress.value = 0;
    try {
      const body = await uploadWithXHR(
        items,
        overwrite,
        (p) => (uploadProgress.value = p),
      );
      if (!body.ok && t(body.error)) throw Error(t(body.error) || t("上传失败"));
      const stored = body.stored || 0;
      const extracted = body.extracted || 0;
      const skipped = body.skipped || 0;
      const parts: string[] = [];
      if (stored) parts.push(t("已上传 ") + stored + t(" 个文件"));
      if (extracted) parts.push(t("解压 ") + extracted + t(" 个文件"));
      if (skipped) parts.push(t("跳过 ") + skipped + t(" 个已存在文件"));
      options.setNote(
        parts.length
          ? parts.join("，") + " · " + t("内容已由 Caddy 实时对外服务")
          : t("没有文件被上传"),
      );
      await options.reload();
    } catch (e) {
      options.setError(e instanceof Error ? e.message : t("上传失败"));
    } finally {
      options.setBusy(false);
      uploadProgress.value = null;
      options.resetInputs();
    }
  }

  function uploadWithXHR(
    items: UploadItem[],
    overwrite: boolean,
    onProgress: (p: number) => void,
  ): Promise<UploadResult> {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open(
        "POST",
        options.api(
          `/upload?path=${encodeURIComponent(options.getPath())}&overwrite=${overwrite ? "1" : "0"}`,
        ),
      );
      xhr.setRequestHeader("X-CSRF-Token", options.getCsrf());
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && e.total > 0) onProgress(e.loaded / e.total);
      };
      xhr.onload = () => {
        try {
          resolve({
            ...JSON.parse(xhr.responseText),
            ok: xhr.status >= 200 && xhr.status < 300,
            status: xhr.status,
          });
        } catch {
          resolve({ ok: false, status: xhr.status, error: "invalid response" });
        }
      };
      xhr.onerror = () => reject(new Error(t("网络错误，上传中断")));
      const data = new FormData();
      for (const item of items) {
        // File names are sent separately from their relative path because the
        // multipart layer strips directory components from a file's name.
        data.append("files", item.file, item.file.name);
        data.append("paths", item.rel);
      }
      xhr.send(data);
    });
  }

  function handleFiles(files: FileList | File[] | null) {
    if (!files || !files.length) return;
    beginUpload(Array.from(files).map((f) => ({ file: f, rel: relPathFor(f) })));
  }

  // Drop payloads prefer the Entry API (folders and nested structure); bare
  // file lists are the fallback for browsers that only expose files.
  async function dropTransfer(dataTransfer: DataTransfer | null) {
    if (dataTransfer?.items?.length) {
      const items = await collectFromItems(dataTransfer.items);
      if (items.length) beginUpload(items);
      return;
    }
    handleFiles(dataTransfer?.files || null);
  }

  return {
    uploadProgress,
    conflictPrompt,
    beginUpload,
    cancelConflict,
    confirmConflict,
    handleFiles,
    dropTransfer,
  };
}
