import { createHash } from "node:crypto"
import { stat } from "node:fs/promises"
import path from "node:path"
import { AppFileSystem } from "@opencode-ai/core/filesystem"

export type Artifact = {
  id: string
  filename: string
  mime?: string
  size_bytes?: number
  relative_path: string
}

export function toPosixRelative(root: string, absolutePath: string) {
  return path.relative(root, absolutePath).split(path.sep).join("/")
}

export function artifactAbsolutePath(root: string, relativePath: string) {
  const normalized = relativePath.replaceAll("\\", "/").trim()
  if (!normalized || normalized === "." || normalized.startsWith("/") || normalized.startsWith("../")) return

  const projectRoot = AppFileSystem.resolve(root)
  const absolutePath = AppFileSystem.resolve(path.join(projectRoot, normalized))
  const resolvedRelative = toPosixRelative(projectRoot, absolutePath)
  if (!resolvedRelative || resolvedRelative === "." || resolvedRelative.startsWith("../")) return

  return absolutePath
}

export async function buildArtifact(root: string, absolutePath: string, options?: { filename?: string }) {
  const projectRoot = AppFileSystem.resolve(root)
  const resolvedPath = AppFileSystem.resolve(absolutePath)
  const relativePath = toPosixRelative(projectRoot, resolvedPath)
  if (!relativePath || relativePath === "." || relativePath.startsWith("../")) {
    throw new Error("artifact path invalid")
  }

  const info = await stat(resolvedPath)
  if (!info.isFile()) {
    throw new Error("artifact not found")
  }

  return {
    id: createHash("sha1").update(relativePath).digest("hex").slice(0, 16),
    filename: options?.filename?.trim() || path.basename(resolvedPath),
    mime: Bun.file(resolvedPath).type || "application/octet-stream",
    size_bytes: info.size,
    relative_path: relativePath,
  } satisfies Artifact
}
