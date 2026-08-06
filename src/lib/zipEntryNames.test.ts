import { describe, expect, it } from "vitest";
import { readZipEntryNames } from "./zipEntryNames";

const CENTRAL_DIRECTORY_SIGNATURE = 0x02014b50;
const END_OF_CENTRAL_DIRECTORY_SIGNATURE = 0x06054b50;

/**
 * Builds just the central directory + End Of Central Directory record for a
 * fake zip containing `names` — enough for readZipEntryNames, which only
 * reads the central directory and never touches local file headers or entry
 * data.
 */
function buildCentralDirectoryZip(names: string[]): Uint8Array<ArrayBuffer> {
  const encoder = new TextEncoder();
  const nameBytesList = names.map((name) => encoder.encode(name));
  const centralDirSize = nameBytesList.reduce((sum, bytes) => sum + 46 + bytes.length, 0);
  const buffer = new Uint8Array(centralDirSize + 22);
  const view = new DataView(buffer.buffer);

  let offset = 0;
  for (const nameBytes of nameBytesList) {
    view.setUint32(offset, CENTRAL_DIRECTORY_SIGNATURE, true);
    view.setUint16(offset + 28, nameBytes.length, true);
    buffer.set(nameBytes, offset + 46);
    offset += 46 + nameBytes.length;
  }

  const eocdOffset = offset;
  view.setUint32(eocdOffset, END_OF_CENTRAL_DIRECTORY_SIGNATURE, true);
  view.setUint16(eocdOffset + 10, names.length, true);
  view.setUint32(eocdOffset + 16, 0, true);

  return buffer;
}

describe("readZipEntryNames", () => {
  it("lists .ics entry names", async () => {
    const zip = buildCentralDirectoryZip(["Personal.ics", "Work.ics"]);
    const file = new File([zip], "export.zip");

    await expect(readZipEntryNames(file)).resolves.toEqual(["Personal.ics", "Work.ics"]);
  });

  it("skips directory entries and non-.ics files", async () => {
    const zip = buildCentralDirectoryZip([
      "calendars/",
      "calendars/Personal.ics",
      "README.txt",
      "__MACOSX/._Personal.ics",
    ]);
    const file = new File([zip], "export.zip");

    await expect(readZipEntryNames(file)).resolves.toEqual([
      "calendars/Personal.ics",
      "__MACOSX/._Personal.ics",
    ]);
  });

  it("matches case-insensitively on the .ics extension", async () => {
    const zip = buildCentralDirectoryZip(["Personal.ICS"]);
    const file = new File([zip], "export.zip");

    await expect(readZipEntryNames(file)).resolves.toEqual(["Personal.ICS"]);
  });

  it("throws when the file has no End Of Central Directory record", async () => {
    const file = new File([new Uint8Array([1, 2, 3, 4])], "not-a-zip.zip");

    await expect(readZipEntryNames(file)).rejects.toThrow("Not a valid zip file.");
  });
});
