import { reactive } from "vue";
import type { AttachmentUploadError, AttachmentUploadResult } from "@copilotkit/shared";
import { showToast } from "./toast.ts";

export type AttachJob = {
  key: string;
  name: string;
  size: number;
  mime: string;
  preview: string;
  progress: number;
  indeterminate: boolean;
  error: string;
};

export const attachJobs = reactive<AttachJob[]>([]);

export function attachKey(name: string, size: number): string {
  return name + "\0" + size;
}

export function fileAttachKey(file: File): string {
  return attachKey(file.name, file.size);
}

export function jobOf(name: string, size: number): AttachJob | undefined {
  const key = attachKey(name, size);
  return attachJobs.find((j) => j.key === key);
}

export function dropAttachJob(key: string): void {
  const i = attachJobs.findIndex((j) => j.key === key);
  if (i < 0) return;
  const job = attachJobs[i];
  if (job.preview) URL.revokeObjectURL(job.preview);
  attachJobs.splice(i, 1);
}

export function clearAttachJobs(): void {
  while (attachJobs.length) dropAttachJob(attachJobs[0].key);
}

export function syncAttachJobs(items: { filename?: string; size?: number; status?: string }[]): void {
  for (const job of attachJobs.slice()) {
    if (job.error) continue;
    const hit = items.find((a) => attachKey(a.filename || "", a.size || 0) === job.key);
    if (!hit || hit.status === "ready") dropAttachJob(job.key);
  }
}

export function explainAttachError(err: AttachmentUploadError): string {
  const name = (err.file && err.file.name) || "文件";
  if (err.reason === "file-too-large") return "「" + name + "」超过 20MB";
  if (err.reason === "invalid-type") return "「" + name + "」类型不支持";
  const raw = String(err.message || "").replace(/\s+/g, " ").trim();
  if (/failed to read|not readable|abort/i.test(raw)) return "「" + name + "」读取失败";
  return "「" + name + "」上传失败";
}

export function onComposerUploadFailed(err: AttachmentUploadError): void {
  const text = explainAttachError(err);
  const file = err.file;
  if (file) {
    const key = fileAttachKey(file);
    let job = attachJobs.find((j) => j.key === key);
    if (!job) {
      job = {
        key,
        name: file.name,
        size: file.size,
        mime: file.type || "",
        preview: file.type.startsWith("image/") ? URL.createObjectURL(file) : "",
        progress: 0,
        indeterminate: false,
        error: text,
      };
      attachJobs.push(job);
    } else {
      job.error = text;
      job.indeterminate = false;
    }
  }
  showToast(text, "error");
}

export function uploadComposerFile(file: File): Promise<AttachmentUploadResult> {
  const key = fileAttachKey(file);
  let job = attachJobs.find((j) => j.key === key);
  if (!job) {
    job = {
      key,
      name: file.name,
      size: file.size,
      mime: file.type || "",
      preview: file.type.startsWith("image/") ? URL.createObjectURL(file) : "",
      progress: 0,
      indeterminate: true,
      error: "",
    };
    attachJobs.push(job);
  } else {
    job.error = "";
    job.progress = 0;
    job.indeterminate = true;
  }
  const current = job;
  return readFileData(file, (loaded, total) => {
    if (total > 0) {
      current.indeterminate = false;
      current.progress = Math.max(0, Math.min(100, Math.round((loaded / total) * 100)));
    }
  }).then((value) => {
    current.progress = 100;
    current.indeterminate = false;
    return {
      type: "data" as const,
      value,
      mimeType: file.type || "application/octet-stream",
    };
  });
}

function readFileData(file: File, onProgress: (loaded: number, total: number) => void): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onprogress = (ev) => {
      if (ev.lengthComputable) onProgress(ev.loaded, ev.total);
    };
    reader.onload = () => {
      const result = String(reader.result || "");
      const i = result.indexOf(",");
      const base64 = i >= 0 ? result.slice(i + 1) : result;
      if (!base64) {
        reject(new Error("Failed to read file as base64"));
        return;
      }
      onProgress(file.size, file.size);
      resolve(base64);
    };
    reader.onerror = () => {
      reject(reader.error || new Error("Failed to read file as base64"));
    };
    reader.readAsDataURL(file);
  });
}
