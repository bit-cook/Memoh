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
- TGS is decompressed and validated as Lottie JSON, then rendered in the Server
  process through rlottie's prebuilt C API, called from Go with `purego`
  (without CGO). It uses the same sampling, size
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
operations. Preparation uses a 15-second context deadline including admission;
each application attachment batch has a 30-second context deadline. FFmpeg
subprocesses are terminated on cancellation. TGS checks cancellation before and
after parsing/rendering calls and between frames; an executing synchronous
rlottie call cannot be interrupted by Go context cancellation.

Video extraction uses bounded subprocess output, private temporary directories
removed on return, restricted input protocols/formats and a 64 MiB individual
allocation limit. TGS stays in memory and does not launch a helper process.
There are no process-wide resource-limit changes to the Server. A native
renderer crash would affect the Server process, so its container resource limits
remain the execution boundary.

`docker/Dockerfile.server` installs ffmpeg/ffprobe with libvpx and the prebuilt
rlottie runtime library. Go loads rlottie once for the process lifetime, disables
its model cache, and creates/destroys an animation for each uncached conversion.
The application retains its bot-scoped frame cache. Pixel compositing and PNG
encoding use Go's image libraries. There is no repository-owned C/C++ source,
helper binary or C/C++ compilation stage for this pipeline. rlottie itself
remains a native runtime dependency.

The native binding supports Linux amd64 and arm64. Source-based deployments need
ffmpeg/ffprobe on the Server's PATH and `librlottie.so.0` on the dynamic loader
path. Missing libraries or unsupported platforms return an unavailable-renderer
error and use the existing attachment fallback.

The binaries still build with `CGO_ENABLED=0`, but importing `purego` makes the
Linux binaries dynamically linked to the system loader. Docker explicitly uses
Alpine's musl loader for Server and Channel. On glibc-based Linux, the default Go
loader is appropriate. For a source build on Alpine:

```sh
apk add --no-cache rlottie ffmpeg
case "$(uname -m)" in
  x86_64) loader=x86_64 ;;
  aarch64) loader=aarch64 ;;
  *) exit 1 ;;
esac
CGO_ENABLED=0 go build -ldflags "-I /lib/ld-musl-${loader}.so.1" -o memoh-server ./cmd/agent
CGO_ENABLED=0 go build -ldflags "-I /lib/ld-musl-${loader}.so.1" -o memoh-channel ./cmd/channel
```

## Verification

Run the regular Go tests for channel ingress, timeline rendering, attachment
routing and media preparation. Go CI also builds the actual `sticker-runtime`
stage and runs synthetic WebM/TGS integration tests with network disabled. The
commands below target an amd64 Alpine container:

```sh
docker build --target sticker-runtime -f docker/Dockerfile.server -t memoh-sticker-tests .
CGO_ENABLED=0 go test -c -ldflags "-I /lib/ld-musl-x86_64.so.1" -o /tmp/vision.test ./internal/media/vision
CGO_ENABLED=0 go test -c -ldflags "-I /lib/ld-musl-x86_64.so.1" -o /tmp/application.test ./internal/agent/application
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
