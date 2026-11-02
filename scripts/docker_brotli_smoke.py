"""Exercise both Brotli decoders inside the actual, network-isolated image."""

import ctypes
import json
import subprocess

from playwright._impl._driver import compute_driver_executable


# Node's upstream CVE-2026-111497 regression input, also used against libbrotli:
# nodejs/node b5f41529df78a8af7b395abd6a97d2dcb6c52c61.
INVALID_BLOCK_SWITCH = bytes.fromhex("200010414141fcffff07002228f00b60f7f41f000000")
PAYLOAD = b"Lake Pass Bot Brotli smoke\n0123456789" * 4

NODE_CONTROLS = r"""
const assert = require('node:assert/strict');
const zlib = require('node:zlib');
const invalid = Buffer.from(process.argv[1], 'hex');
const payload = Buffer.from(process.argv[2], 'hex');
const expected = { code: 'ERR__ERROR_FORMAT_BLOCK_SWITCH', errno: -17 };

(async () => {
  // The security backport retains Brotli's 1.2.0 display version. Observe
  // the new permanent format error, including incremental input, instead.
  assert.equal(zlib.constants.BROTLI_DECODER_ERROR_FORMAT_BLOCK_SWITCH, -17);
  assert.throws(() => zlib.brotliDecompressSync(invalid), expected);
  let malformedOutputBytes = 0;
  await new Promise((resolve, reject) => {
    const decoder = zlib.createBrotliDecompress();
    decoder.resume();
    decoder.on('data', (chunk) => { malformedOutputBytes += chunk.length; });
    decoder.on('error', (error) => {
      try {
        assert.equal(error.code, expected.code);
        assert.equal(error.errno, expected.errno);
        resolve();
      } catch (failure) {
        reject(failure);
      }
    });
    decoder.on('end', () => reject(new Error('malformed stream was accepted')));
    for (const byte of invalid) decoder.write(Buffer.from([byte]));
    decoder.end();
  });
  const compressed = zlib.brotliCompressSync(payload);
  assert.deepEqual(zlib.brotliDecompressSync(compressed), payload);
  assert.throws(() => zlib.brotliDecompressSync(compressed.subarray(0, -1)));
  console.log(JSON.stringify({
    node: process.versions.node, brotliDisplayVersion: process.versions.brotli,
    compressed: compressed.toString('hex'), malformedOutputBytes,
  }));
})().catch((error) => { console.error(error); process.exitCode = 1; });
"""


def main():
    node, _ = compute_driver_executable()
    result = subprocess.run(
        [node, "-e", NODE_CONTROLS, INVALID_BLOCK_SWITCH.hex(), PAYLOAD.hex()],
        check=True, stdout=subprocess.PIPE, text=True, timeout=10,
    )
    report = json.loads(result.stdout)
    compressed = bytes.fromhex(report.pop("compressed"))

    byte_pointer = ctypes.POINTER(ctypes.c_uint8)
    size_pointer = ctypes.POINTER(ctypes.c_size_t)
    library = ctypes.CDLL("libbrotlidec.so.1")
    library.BrotliDecoderDecompress.argtypes = [
        ctypes.c_size_t, byte_pointer, size_pointer, byte_pointer,
    ]
    library.BrotliDecoderDecompress.restype = ctypes.c_int

    def decompress(data):
        source = ctypes.create_string_buffer(data)
        output = ctypes.create_string_buffer(len(PAYLOAD) + 16)
        output_size = ctypes.c_size_t(len(output))
        status = library.BrotliDecoderDecompress(
            len(data), ctypes.cast(source, byte_pointer),
            ctypes.byref(output_size), ctypes.cast(output, byte_pointer),
        )
        return status, output.raw[:output_size.value]

    status, decoded = decompress(compressed)
    assert status == 1 and decoded == PAYLOAD, "native valid-input control failed"
    assert decompress(compressed[:-1])[0] == 0, "native truncated input was accepted"

    library.BrotliDecoderCreateInstance.argtypes = [ctypes.c_void_p] * 3
    library.BrotliDecoderCreateInstance.restype = ctypes.c_void_p
    library.BrotliDecoderDestroyInstance.argtypes = [ctypes.c_void_p]
    library.BrotliDecoderGetErrorCode.argtypes = [ctypes.c_void_p]
    library.BrotliDecoderGetErrorCode.restype = ctypes.c_int
    library.BrotliDecoderDecompressStream.argtypes = [
        ctypes.c_void_p, size_pointer, ctypes.POINTER(byte_pointer),
        size_pointer, ctypes.POINTER(byte_pointer), size_pointer,
    ]
    library.BrotliDecoderDecompressStream.restype = ctypes.c_int
    state = library.BrotliDecoderCreateInstance(None, None, None)
    assert state, "native decoder allocation failed"
    try:
        source = ctypes.create_string_buffer(INVALID_BLOCK_SWITCH)
        output = ctypes.create_string_buffer(16384)
        remaining_input = ctypes.c_size_t(len(INVALID_BLOCK_SWITCH))
        next_input = ctypes.cast(source, byte_pointer)
        produced = 0
        calls = 0
        while True:
            remaining_output = ctypes.c_size_t(len(output))
            next_output = ctypes.cast(output, byte_pointer)
            status = library.BrotliDecoderDecompressStream(
                state, ctypes.byref(remaining_input), ctypes.byref(next_input),
                ctypes.byref(remaining_output), ctypes.byref(next_output), None,
            )
            produced += len(output) - remaining_output.value
            calls += 1
            # A Brotli metablock can expand to 16 MiB before the invalid
            # switch. Bound this fixed upstream fixture to two such blocks.
            assert produced <= 32 * 1024 * 1024 and calls <= 2049, (
                f"native fixture output budget exceeded: {produced} bytes, {calls} calls"
            )
            # NEEDS_MORE_OUTPUT is not success or malformed-input rejection.
            # Drain bounded chunks so the decoder can reach the invalid switch.
            if status != 3:
                break
        assert status == 0, (
            f"native malformed block switch was not a permanent error: "
            f"status={status}, output={produced}"
        )
        # The C API only promises a detailed error after RESULT_ERROR.
        error_code = library.BrotliDecoderGetErrorCode(state)
        assert error_code == -17, f"unexpected native format error: {error_code}"
    finally:
        library.BrotliDecoderDestroyInstance(state)
    print(json.dumps({
        **report, "nativeDecoder": "block-switch rejected", "controls": "passed",
        "nativeCalls": calls, "nativeOutputBytes": produced,
    }))


if __name__ == "__main__":
    main()
