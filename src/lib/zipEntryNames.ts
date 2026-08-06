const CENTRAL_DIRECTORY_SIGNATURE = 0x02014b50;
const END_OF_CENTRAL_DIRECTORY_SIZE = 22;

/**
 * Reads just the entry filenames out of a .zip's central directory — enough
 * to build one import target per entry before the very first dryRun preview
 * request, which the backend requires for every file it finds inside the
 * archive (issue #77's ErrImportTargetMissing: every entry must have a
 * matching target by filename, or the whole request is rejected). Skips
 * directory entries and anything that isn't a .ics file, mirroring the
 * backend's own filter in ExtractFiles (server/internal/service/import.go)
 * so the names line up exactly with what it will actually import.
 */
export async function readZipEntryNames(file: Blob): Promise<string[]> {
  const buffer = await file.arrayBuffer();
  const view = new DataView(buffer);
  const bytes = new Uint8Array(buffer);

  const eocdOffset = findEndOfCentralDirectory(bytes);
  if (eocdOffset === -1) {
    throw new Error("Not a valid zip file.");
  }

  const entryCount = view.getUint16(eocdOffset + 10, true);
  let offset = view.getUint32(eocdOffset + 16, true);

  const decoder = new TextDecoder();
  const names: string[] = [];
  for (let i = 0; i < entryCount; i++) {
    if (view.getUint32(offset, true) !== CENTRAL_DIRECTORY_SIGNATURE) {
      throw new Error("Malformed zip central directory.");
    }

    const nameLength = view.getUint16(offset + 28, true);
    const extraLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    const nameStart = offset + 46;
    const name = decoder.decode(bytes.slice(nameStart, nameStart + nameLength));

    if (!name.endsWith("/") && name.toLowerCase().endsWith(".ics")) {
      names.push(name);
    }

    offset = nameStart + nameLength + extraLength + commentLength;
  }

  return names;
}

/**
 * The End Of Central Directory record's offset: found by scanning backward
 * from the end, since it may be followed by a variable-length zip comment.
 */
function findEndOfCentralDirectory(bytes: Uint8Array): number {
  for (let i = bytes.length - END_OF_CENTRAL_DIRECTORY_SIZE; i >= 0; i--) {
    if (bytes[i] === 0x50 && bytes[i + 1] === 0x4b && bytes[i + 2] === 0x05 && bytes[i + 3] === 0x06) {
      return i;
    }
  }
  return -1;
}
