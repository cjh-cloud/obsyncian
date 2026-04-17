import {
  S3Client,
  ListObjectsV2Command,
  GetObjectCommand,
  PutObjectCommand,
  DeleteObjectCommand,
} from '@aws-sdk/client-s3';
import { Upload } from '@aws-sdk/lib-storage';
import RNFS from 'react-native-fs';
import { md5 } from '@noble/hashes/legacy.js';
import { bytesToHex } from '@noble/hashes/utils.js';
import { ObsyncianConfig } from '../models/config';
import { fileStatCache } from './fileStatCache';

export interface FileInfo {
  path: string; // relative path
  etag: string; // local: MD5 hex; remote: S3 ListObjects ETag (quotes stripped), same as Flutter
  size: number;
  /** Local file mtime (ms) or undefined for remote-only entries */
  lastModifiedMs?: number;
  /** S3 object LastModified (ms) when listed from S3 */
  remoteLastModifiedMs?: number;
}

export interface SyncDiff {
  toUpload: FileInfo[];
  toDownload: FileInfo[];
  toDelete: FileInfo[];
}

export interface DryRunResult {
  diff: SyncDiff;
  localFiles: FileInfo[];
  remoteFiles: FileInfo[];
}

/** Match Flutter: only skip the `.git` directory segment, not arbitrary `.git` substrings. */
function shouldSkipRelativePath(relativePath: string): boolean {
  return relativePath.split('/').some((segment) => segment === '.git');
}

function statMtimeMs(stat: { mtime?: Date | number }): number {
  const m = stat.mtime;
  if (m instanceof Date) return m.getTime();
  if (typeof m === 'number') return m;
  return 0;
}

/**
 * Base64 string → bytes without `Buffer.from(str, 'base64')`.
 * react-native-quick-crypto's Buffer can throw "undefined is not a function" in that decode path on Hermes.
 */
function bytesFromBase64String(b64: string): Uint8Array {
  const s = String(b64).replace(/\s/g, '');
  if (s.length === 0) {
    return new Uint8Array(0);
  }
  const atobFn = globalThis.atob;
  if (typeof atobFn !== 'function') {
    throw new Error('globalThis.atob is not available for base64 decoding');
  }
  const binary = atobFn(s);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i) & 0xff;
  }
  return out;
}

/** Strip quotes and weak ETag prefix from S3 ListObjects ETag. */
function normalizeS3Etag(etag: string): string {
  let s = etag.trim();
  if (s.startsWith('W/') || s.startsWith('w/')) {
    s = s.slice(2).trim();
  }
  return s.replace(/^"|"$/g, '').trim().toLowerCase();
}

/**
 * Multipart S3 ETag is `md5hex-<partCount>`, not the object MD5. Match Flutter/Go-style skip:
 * same size and local mtime not older than remote LastModified ⇒ treat as unchanged.
 */
function isMultipartS3Etag(etagNorm: string): boolean {
  return /^[a-f0-9]{32}-\d+$/i.test(etagNorm);
}

/** True if local file already matches remote object (skip download / skip upload in dry-run). */
function localMatchesRemote(local: FileInfo, remote: FileInfo): boolean {
  const rtag = normalizeS3Etag(remote.etag);
  const ltag = local.etag.trim().toLowerCase();
  if (!rtag) return false;

  if (isMultipartS3Etag(rtag)) {
    if (local.size !== remote.size) return false;
    if (
      remote.remoteLastModifiedMs != null &&
      local.lastModifiedMs != null &&
      local.lastModifiedMs < remote.remoteLastModifiedMs
    ) {
      return false;
    }
    return true;
  }

  return ltag === rtag;
}

export type SyncProgressCallback = (progress: SyncProgress) => void;

export interface SyncProgress {
  phase: 'listing' | 'uploading' | 'downloading' | 'deleting' | 'done';
  current: number;
  total: number;
  /** The file currently being processed (if applicable). */
  file?: string;
}

class S3SyncService {
  private s3Client: S3Client | null = null;
  private bucketName: string = '';
  private vaultPath: string = '';
  private lastLog: (msg: string) => void = () => {};
  private onProgress: SyncProgressCallback | null = null;

  async init(config: ObsyncianConfig, vaultPath: string, onLog: (msg: string) => void): Promise<void> {
    this.bucketName = config.cloud;
    this.vaultPath = vaultPath;
    this.lastLog = onLog;

    this.s3Client = new S3Client({
      region: config.region,
      credentials: {
        accessKeyId: config.credentials.key,
        secretAccessKey: config.credentials.secret,
      },
      responseChecksumValidation: 'WHEN_REQUIRED',
    });
  }

  setOnProgress(callback: SyncProgressCallback | null): void {
    this.onProgress = callback;
  }

  private emitProgress(progress: SyncProgress): void {
    if (this.onProgress) {
      try {
        this.onProgress(progress);
      } catch {
        // Non-fatal
      }
    }
  }

  async syncUp(precomputed?: DryRunResult): Promise<void> {
    if (!this.s3Client) throw new Error('S3SyncService not initialized');

    this.lastLog('[S3] Starting sync up (local → S3)');

    let toUpload: FileInfo[];
    let toDeleteRemote: FileInfo[];

    if (precomputed) {
      toUpload = precomputed.diff.toUpload;
      toDeleteRemote = precomputed.diff.toDelete;
    } else {
      const result = await this.dryRun();
      toUpload = result.diff.toUpload;
      toDeleteRemote = result.diff.toDelete;
    }

    const totalOps = toUpload.length + toDeleteRemote.length;
    let completed = 0;

    for (const file of toUpload) {
      const fullPath = `${this.vaultPath}/${file.path}`;
      this.lastLog(`[S3] Uploading: ${file.path}`);
      this.emitProgress({ phase: 'uploading', current: completed + 1, total: totalOps, file: file.path });

      try {
        const fileContent = await RNFS.readFile(fullPath, 'base64');
        const uploader = new Upload({
          client: this.s3Client,
          params: {
            Bucket: this.bucketName,
            Key: file.path,
            Body: bytesFromBase64String(fileContent),
          },
        });

        await uploader.done();
        completed++;
      } catch (error) {
        this.lastLog(`[S3] Upload failed: ${file.path}: ${error}`);
        throw error;
      }
    }

    for (const file of toDeleteRemote) {
      this.lastLog(`[S3] Deleting from S3: ${file.path}`);
      this.emitProgress({ phase: 'deleting', current: completed + 1, total: totalOps, file: file.path });

      try {
        await this.s3Client.send(
          new DeleteObjectCommand({
            Bucket: this.bucketName,
            Key: file.path,
          }),
        );
        completed++;
      } catch (error) {
        this.lastLog(`[S3] Delete failed: ${file.path}: ${error}`);
        throw error;
      }
    }

    this.emitProgress({ phase: 'done', current: totalOps, total: totalOps });
    this.lastLog('[S3] Sync up complete');
  }

  async syncDown(): Promise<void> {
    if (!this.s3Client) throw new Error('S3SyncService not initialized');

    this.lastLog('[S3] Starting sync down (S3 → local)');
    this.emitProgress({ phase: 'listing', current: 0, total: 0 });

    const remoteFiles = await this.listS3Objects();
    const localFiles = await this.listLocalFiles();

    // Build lists of work
    const toDownload = remoteFiles.filter((remote) => {
      const local = localFiles.find((f) => f.path === remote.path);
      return !local || !localMatchesRemote(local, remote);
    });
    const toDeleteLocal = localFiles.filter(
      (local) => !remoteFiles.find((r) => r.path === local.path),
    );

    const totalOps = toDownload.length + toDeleteLocal.length;
    let completed = 0;

    // Download new/changed files
    for (const remote of toDownload) {
      this.lastLog(`[S3] Downloading: ${remote.path}`);
      this.emitProgress({ phase: 'downloading', current: completed + 1, total: totalOps, file: remote.path });

      try {
        const fullPath = `${this.vaultPath}/${remote.path}`;

        // Create directories
        const dir = fullPath.substring(0, fullPath.lastIndexOf('/'));
        await RNFS.mkdir(dir);

        // Download file
        const response = await this.s3Client.send(
          new GetObjectCommand({
            Bucket: this.bucketName,
            Key: remote.path,
          }),
        );

        const b64 = await this.responseBodyToBase64(response.Body);
        await RNFS.writeFile(fullPath, b64, 'base64');

        // Update stat cache
        const stat = await RNFS.stat(fullPath);
        fileStatCache.set(remote.path, {
          mtime: statMtimeMs(stat),
          size: Number(stat.size) || 0,
          md5: remote.etag,
        });
        completed++;
      } catch (error) {
        this.lastLog(`[S3] Download failed: ${remote.path}: ${error}`);
        throw error;
      }
    }

    // Delete local files not in S3
    for (const local of toDeleteLocal) {
      const fullPath = `${this.vaultPath}/${local.path}`;
      this.lastLog(`[S3] Deleting local file: ${local.path}`);
      this.emitProgress({ phase: 'deleting', current: completed + 1, total: totalOps, file: local.path });

      try {
        await RNFS.unlink(fullPath);
        fileStatCache.set(local.path, undefined as any); // Remove from cache
        completed++;
      } catch (error) {
        this.lastLog(`[S3] Delete local failed: ${local.path}: ${error}`);
        throw error;
      }
    }

    this.emitProgress({ phase: 'done', current: totalOps, total: totalOps });
    this.lastLog('[S3] Sync down complete');
  }

  /**
   * Dry-run sync-up diff (matches Flutter's dryRunSyncUp).
   * Returns the diff plus the raw file lists so syncUp() can reuse them
   * without a second listing pass.
   */
  async dryRun(): Promise<DryRunResult> {
    this.emitProgress({ phase: 'listing', current: 0, total: 0 });
    const remoteFiles = await this.listS3Objects();
    const localFiles = await this.listLocalFiles();

    const localMap = new Map(localFiles.map((f) => [f.path, f]));
    const remoteMap = new Map(remoteFiles.map((f) => [f.path, f]));

    const toUpload: FileInfo[] = [];
    const toDelete: FileInfo[] = [];

    for (const local of localFiles) {
      const remote = remoteMap.get(local.path);
      if (!remote || !localMatchesRemote(local, remote)) {
        toUpload.push(local);
      }
    }

    for (const remote of remoteFiles) {
      if (!localMap.has(remote.path)) {
        toDelete.push(remote);
      }
    }

    return {
      diff: { toUpload, toDownload: [], toDelete },
      localFiles,
      remoteFiles,
    };
  }

  private async listLocalFiles(): Promise<FileInfo[]> {
    const files: FileInfo[] = [];

    const walk = async (dir: string, basePath: string = ''): Promise<void> => {
      try {
        const items = await RNFS.readDir(dir);

        for (const item of items) {
          const relativePath = basePath ? `${basePath}/${item.name}` : item.name;

          if (shouldSkipRelativePath(relativePath)) {
            continue;
          }

          if (item.isDirectory()) {
            await walk(item.path, relativePath);
          } else {
            const stat = await RNFS.stat(item.path);
            const mtimeMs = statMtimeMs(stat);
            const size = Number(stat.size) || 0;

            let md5Hex: string;
            const cached = fileStatCache.get(relativePath);
            if (cached?.md5 && cached.mtime === mtimeMs && cached.size === size) {
              md5Hex = cached.md5;
            } else {
              md5Hex = await this.md5HexOfFileFromDisk(item.path);
              fileStatCache.set(relativePath, { mtime: mtimeMs, size, md5: md5Hex });
            }

            files.push({
              path: relativePath,
              etag: md5Hex,
              size,
              lastModifiedMs: mtimeMs,
            });
          }
        }
      } catch (error) {
        console.error(`Error walking directory ${dir}:`, error);
      }
    };

    await walk(this.vaultPath);
    return files;
  }

  private async listS3Objects(): Promise<FileInfo[]> {
    if (!this.s3Client) throw new Error('S3SyncService not initialized');

    const files: FileInfo[] = [];
    let continuationToken: string | undefined;

    while (true) {
      const response = await this.s3Client.send(
        new ListObjectsV2Command({
          Bucket: this.bucketName,
          ContinuationToken: continuationToken,
          MaxKeys: 1000,
        }),
      );

      if (!response.Contents) break;

      for (const obj of response.Contents) {
        if (!obj.Key) continue;

        // Skip directories
        if (obj.Key.endsWith('/')) continue;

        const etagRaw = obj.ETag ? obj.ETag.replace(/^"|"$/g, '') : '';
        const lm = obj.LastModified;
        const remoteLm =
          lm instanceof Date ? lm.getTime() : lm != null ? new Date(String(lm)).getTime() : undefined;

        files.push({
          path: obj.Key,
          etag: etagRaw,
          size: obj.Size ?? 0,
          remoteLastModifiedMs: remoteLm,
        });
      }

      if (!response.IsTruncated) break;
      continuationToken = response.NextContinuationToken;
    }

    return files;
  }

  /** Convert an S3 response body to a base64 string (RN-safe: no FileReader.readAsArrayBuffer). */
  private async responseBodyToBase64(body: any): Promise<string> {
    if (!body) {
      throw new Error('S3 GetObject returned an empty body');
    }

    // Raw bytes (unusual but cheap)
    if (body instanceof Uint8Array || ArrayBuffer.isView(body)) {
      return Buffer.from(body as Uint8Array).toString('base64');
    }

    // SDK mixin: prefer base64 string (uses streamCollector internally)
    if (typeof body.transformToString === 'function') {
      try {
        return await body.transformToString('base64');
      } catch {
        // Fall through — e.g. checksum/Blob edge cases
      }
    }

    // Blob (Hermes): arrayBuffer() when present; else readAsDataURL (RN implements this, not readAsArrayBuffer)
    if (typeof Blob === 'function' && (body instanceof Blob || body?.constructor?.name === 'Blob')) {
      if (typeof body.arrayBuffer === 'function') {
        const ab = await body.arrayBuffer();
        return Buffer.from(new Uint8Array(ab)).toString('base64');
      }
      return this.blobToBase64ViaDataUrl(body);
    }

    if (typeof body.transformToByteArray === 'function') {
      const bytes: Uint8Array = await body.transformToByteArray();
      return Buffer.from(bytes).toString('base64');
    }

    // Web ReadableStream
    if (typeof body.getReader === 'function') {
      const bytes = await this.readableStreamToUint8Array(body);
      return Buffer.from(bytes).toString('base64');
    }

    throw new Error(`Unexpected S3 response body: ${body?.constructor?.name ?? typeof body}`);
  }

  /** RN FileReader supports readAsDataURL, not readAsArrayBuffer — same approach as @smithy/fetch-http-handler. */
  private async blobToBase64ViaDataUrl(blob: Blob): Promise<string> {
    if (typeof FileReader !== 'function') {
      throw new Error('FileReader is not available to read S3 response body');
    }
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onloadend = () => {
        if (reader.readyState !== 2) {
          reject(new Error('FileReader finished before load completed'));
          return;
        }
        const result = String(reader.result ?? '');
        const comma = result.indexOf(',');
        const payload = comma > -1 ? result.slice(comma + 1) : result;
        resolve(payload);
      };
      reader.onerror = () => reject(reader.error ?? new Error('FileReader error'));
      reader.readAsDataURL(blob);
    });
  }

  private async readableStreamToUint8Array(stream: { getReader: () => any }): Promise<Uint8Array> {
    const reader = stream.getReader();
    const chunks: Uint8Array[] = [];
    let length = 0;
    let done = false;
    while (!done) {
      const step = await reader.read();
      done = !!step.done;
      const value = step.value as Uint8Array | undefined;
      if (value) {
        chunks.push(value);
        length += value.length;
      }
    }
    const out = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) {
      out.set(chunk, offset);
      offset += chunk.length;
    }
    return out;
  }

  /** MD5 hex of on-disk bytes (matches S3 single-part ETag for same bytes). */
  private async md5HexOfFileFromDisk(filePath: string): Promise<string> {
    const b64 = await RNFS.readFile(filePath, 'base64');
    const bytes = bytesFromBase64String(b64);
    return bytesToHex(md5(bytes));
  }

}

export const s3SyncService = new S3SyncService();
