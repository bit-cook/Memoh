// Small out-of-process TGS renderer. The Go caller validates and decompresses
// Lottie JSON, bounds concurrency, and kills this process on deadline.
#include <rlottie.h>
#include <png.h>
#include <sys/resource.h>
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

int main(int argc, char **argv) {
    if (argc != 3) return 2;
    rlimit memory{256 * 1024 * 1024, 256 * 1024 * 1024};
    rlimit cpu{10, 10};
    rlimit fileSize{2 * 1024 * 1024, 2 * 1024 * 1024};
    if (setrlimit(RLIMIT_AS, &memory) || setrlimit(RLIMIT_CPU, &cpu) ||
        setrlimit(RLIMIT_FSIZE, &fileSize)) return 2;
    std::ifstream input(argv[1], std::ios::binary);
    std::string json((std::istreambuf_iterator<char>(input)), {});
    if (json.empty() || json.size() > 2 * 1024 * 1024) return 2;
    auto animation = rlottie::Animation::loadFromData(json, "sticker", "", false);
    if (!animation) return 3;
    size_t width = 0, height = 0;
    animation->size(width, height);
    auto total = animation->totalFrame();
    if (!width || !height || width > 4096 || height > 4096 || !total || total > 7200) return 3;
    double scale = std::min(1.0, 512.0 / std::max(width, height));
    width = std::max<size_t>(1, width * scale);
    height = std::max<size_t>(1, height * scale);
    auto count = std::min<size_t>(5, total);
    std::vector<uint32_t> pixels(width * height);
    std::vector<unsigned char> rgb(width * height * 3);
    for (size_t i = 0; i < count; ++i) {
        size_t frame = count == 1 ? 0 : std::lround(double(i) * (total - 1) / (count - 1));
        std::fill(pixels.begin(), pixels.end(), 0);
        animation->renderSync(frame, rlottie::Surface(pixels.data(), width, height, width * 4));
        for (size_t j = 0; j < pixels.size(); ++j) {
            // rlottie uses premultiplied ARGB. Composite on white before PNG.
            auto p = pixels[j];
            auto white = 255 - ((p >> 24) & 255);
            rgb[j * 3] = std::min<uint32_t>(255, ((p >> 16) & 255) + white);
            rgb[j * 3 + 1] = std::min<uint32_t>(255, ((p >> 8) & 255) + white);
            rgb[j * 3 + 2] = std::min<uint32_t>(255, (p & 255) + white);
        }
        png_image image{};
        image.version = PNG_IMAGE_VERSION;
        image.width = width;
        image.height = height;
        image.format = PNG_FORMAT_RGB;
        auto path = std::string(argv[2]) + "/frame_" + std::to_string(i + 1) + ".png";
        if (!png_image_write_to_file(&image, path.c_str(), 0, rgb.data(), 0, nullptr)) {
            png_image_free(&image);
            return 4;
        }
        png_image_free(&image);
    }
    std::cout << count << '\n';
    return 0;
}
