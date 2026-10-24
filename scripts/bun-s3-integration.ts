const endpoint = process.env.LOCAL_S3_TEST_ENDPOINT;
const accessKeyId = process.env.LOCAL_S3_TEST_ACCESS_KEY;
const secretAccessKey = process.env.LOCAL_S3_TEST_SECRET_KEY;
const bucket = process.env.LOCAL_S3_TEST_BUCKET ?? "gati-media";

if (!endpoint || !accessKeyId || !secretAccessKey) {
  throw new Error("Set LOCAL_S3_TEST_ENDPOINT, LOCAL_S3_TEST_ACCESS_KEY, and LOCAL_S3_TEST_SECRET_KEY");
}

const origin = "http://localhost:8081";
const client = new Bun.S3Client({
  endpoint,
  region: "us-east-1",
  accessKeyId,
  secretAccessKey,
  bucket,
});

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}

function equalBytes(actual: ArrayBuffer, expected: Uint8Array, label: string) {
  const bytes = new Uint8Array(actual);
  assert(
    bytes.length === expected.length && bytes.every((byte, index) => byte === expected[index]),
    `${label}: bytes did not match`,
  );
}

function headerIncludes(headers: string | null, expected: string): boolean {
  return (headers ?? "").split(",").some((value) => value.trim().toLowerCase() === expected.toLowerCase());
}

try {
  const objectKey = `bun-s3-integration-${crypto.randomUUID()}.bin`;
  const objectBytes = new TextEncoder().encode("Bun S3 integration payload\u0000\u00ff");
  await client.write(objectKey, objectBytes, { type: "application/octet-stream" });
  console.log("PASS Bun.s3.write stored object");

  const metadata = await client.stat(objectKey);
  assert(metadata.size === objectBytes.byteLength, `stat size was ${metadata.size}, expected ${objectBytes.byteLength}`);
  assert(metadata.type === "application/octet-stream", `stat type was ${metadata.type}`);
  equalBytes(await client.file(objectKey).arrayBuffer(), objectBytes, "Bun.s3.file readback");
  console.log("PASS stat metadata and file readback");

  const presignedKey = `bun-s3-presigned-${crypto.randomUUID()}.bin`;
  const presignedBytes = new TextEncoder().encode("presigned upload payload\u0000\u00fe");
  const presignedUrl = await client.presign(presignedKey, { method: "PUT", expiresIn: 300 });
  assert(typeof presignedUrl === "string", "Bun.s3.presign did not return a URL");

  const preflight = await fetch(presignedUrl, {
    method: "OPTIONS",
    headers: {
      Origin: origin,
      "Access-Control-Request-Method": "PUT",
      "Access-Control-Request-Headers": "content-type",
    },
  });
  assert(preflight.ok, `PUT preflight returned HTTP ${preflight.status}`);
  assert(preflight.headers.get("access-control-allow-origin") === origin, "PUT preflight ACAO did not match origin");
  assert(headerIncludes(preflight.headers.get("access-control-allow-methods"), "PUT"), "PUT preflight ACAM did not include PUT");
  assert(headerIncludes(preflight.headers.get("access-control-allow-headers"), "content-type"), "PUT preflight ACAH did not include content-type");
  console.log("PASS unauthenticated PUT CORS preflight");

  const uploaded = await fetch(presignedUrl, {
    method: "PUT",
    headers: { "Content-Type": "application/octet-stream" },
    body: presignedBytes,
  });
  assert(uploaded.status === 200, `presigned PUT returned HTTP ${uploaded.status}`);
  const uploadedMetadata = await client.stat(presignedKey);
  assert(uploadedMetadata.size === presignedBytes.byteLength, "presigned upload stat size did not match");
  equalBytes(await client.file(presignedKey).arrayBuffer(), presignedBytes, "presigned upload readback");
  console.log("PASS unauthenticated presigned PUT and exact readback");
} catch (error) {
  const message = error instanceof Error ? error.message : "unknown error";
  // Never emit capability URLs or credential material in diagnostics.
  console.error(`FAIL Bun S3 integration: ${message.replace(/https?:\/\/\S+/g, "[redacted URL]")}`);
  process.exitCode = 1;
}
