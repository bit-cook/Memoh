# Sticker media and model input

Telegram video and animated stickers must be decoded before they enter an
image-only model request. Labeling WebM or gzipped Lottie bytes as an image does
not make them a supported raster format. A malformed attachment retained in a
discussion's pending context can otherwise fail each subsequent model call.

## Channel ownership and flow

| Stage | Responsibility |
| --- | --- |
| `internal/channel/adapters/telegram` | Recognize static, video and animated stickers as `sticker`; retain WebP, WebM or TGS MIME and Telegram file reference. |
| `internal/channel` and `internal/channel/inbound` | Normalize attachments, download platform media and persist the original through the media store. Forward content hash, MIME, dimensions and file reference. |
| `internal/chat/timeline` | Render attachment text and collect image/sticker references. This layer performs no storage access, decoding or model calls. |
| `internal/agent/application` | Resolve media with the bot-scoped asset loader. Prepare images for ordinary chat, discussion context and messages injected into an active run. The External Agent attachment path shares ordinary chat preparation. |
| `internal/media/vision` | Inspect bytes, validate raster images and turn animation into bounded PNG frames. |
| Native model/runtime adapters | Receive supported images; the existing chat capability and size router retains file fallbacks for models without vision. |

The original file and its content hash remain the source of truth. Derived frames
are ephemeral model input, not replacement media records. No schema migration or
history backfill is required. Previously stored `image` attachments containing
WebM or gzip/TGS are recognized from their bytes, even if their MIME is wrong.
Other channels continue using the same attachment contract; conversion occurs
only when an attachment is entering the image/sticker input path.

## Preparation behavior

- Static stickers are decoded, fit within 512 × 512 without enlargement and
  composited on white. Ordinary JPEG, PNG and WebP images are decoded for
  validation and retain their original bytes.
- WebM, MP4 and GIF animations are sampled at up to five evenly spaced frame
  indices, including the first and last. Short animations use all frames.
  `ffprobe` reads the frame count, falling back to counting decoded frames.
  `ffmpeg` extracts and scales PNG frames. VP8/VP9 use libvpx so transparent
  WebM pixels survive decoding and can be composited on white.
- TGS is decompressed and validated as Lottie JSON, then rendered by the separate
  `memoh-sticker-render` process using rlottie. It uses the same sampling, size
  and white-background rules. Bitmap asset references are rejected.
- Identical derived frames are deduplicated. A process-local cache keys successful
  results by bot scope, source SHA-256 and sticker treatment; it holds at most
  64 entries and 16 MiB of frame bytes. Failed conversions are not cached.
- A failed conversion logs a diagnostic without source URLs or file contents.
  Discussion/injected image input omits it, while the original attachment text
  remains available. Ordinary chat uses the existing file-reference fallback.
  External Agents receive the existing structured invalid/unavailable error
  when there is no reachable fallback. Invalid bytes are never resent through
  the original image URL after a failed stored-media conversion.
- Unpersisted public raster-image URLs retain their existing direct-input
  behavior. The converter does not fetch arbitrary URLs. URL-only animated
  stickers need ingestion before they can become model images.

These frames go to the existing vision-capable model. This change does not add a
separate caption model, persistent alt-text cache or automatic history rewrite.

## Resource bounds and runtime dependencies

Input is limited to 20 MiB; decoded raster dimensions to 16 megapixels; TGS JSON
to 2 MiB and canvas dimensions to 4096 on each axis. Animations are limited to
7200 frames and, when video duration metadata is available, 60 seconds; TGS
always enforces the duration limit. A processor permits two concurrent decoding
operations, with a 15-second deadline including admission to that limit. Each
application attachment batch has a 30-second preparation deadline.

Subprocesses have bounded diagnostic output and private temporary directories
removed on return. FFmpeg input protocols/formats are restricted to local media
containers, individual allocations to 64 MiB, and decoder threads to one. The
TGS helper additionally limits address space to 256 MiB, CPU time to 10 seconds
and each output file to 2 MiB. These are resource bounds, not an OS sandbox for
native codecs; production container limits remain appropriate.

`docker/Dockerfile.server` includes ffmpeg/ffprobe with libvpx and builds the TGS
helper against rlottie and libpng. The Go server remains CGO-free. Source-based
Linux deployments also need these programs on the **server's** PATH. For Alpine,
the helper can be built with:

```sh
apk add --no-cache g++ rlottie-dev libpng-dev ffmpeg
c++ -std=c++17 -O2 -o /usr/local/bin/memoh-sticker-render \
  docker/sticker-renderer/main.cpp -lrlottie -lpng
```

If a decoder is missing or rejects an input, the file fallback still applies;
animation vision is unavailable for that input. The native helper is built for
the target platform by Docker, separately from the cross-compiled Go binaries.

## Verification

Run the regular Go tests for channel ingress, timeline rendering, attachment
routing and media preparation. Go CI also builds the actual `sticker-runtime`
stage and runs synthetic WebM/TGS integration tests with network disabled:

```sh
docker build --target sticker-runtime -f docker/Dockerfile.server -t memoh-sticker-tests .
CGO_ENABLED=0 go test -c -o /tmp/vision.test ./internal/media/vision
CGO_ENABLED=0 go test -c -o /tmp/application.test ./internal/agent/application
docker run --rm --network none --memory 768m --cpus 2 --pids-limit 128 \
  -e MEMOH_TEST_MEDIA_DECODERS=1 -v /tmp/vision.test:/vision.test:ro \
  memoh-sticker-tests /vision.test -test.v
docker run --rm --network none --memory 768m --cpus 2 --pids-limit 128 \
  -e MEMOH_TEST_MEDIA_DECODERS=1 -v /tmp/application.test:/application.test:ro \
  memoh-sticker-tests /application.test -test.v -test.run TestAnimatedStickerEntryPoints
```

The integration tests generate their own media; they contain no user files.
They check bounded frames, alpha compositing, deduplication, and consistent
model input across ordinary chat, discussion and injection, including historical
labels. `MEMOH_TEST_MEDIA_DECODERS=1` makes absent decoders fail instead of skip.
These checks do not replace human QA with a real Telegram bot and model provider.
