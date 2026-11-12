#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

#include "bridge/kilnfile_bridge.h"

#define CHECK(cond)                                                      \
  do {                                                                   \
    if (!(cond)) {                                                       \
      std::cerr << __FILE__ << ":" << __LINE__ << " CHECK failed: " #cond \
                << std::endl;                                            \
      std::exit(1);                                                      \
    }                                                                    \
  } while (0)

int main() {
  constexpr uint32_t kW = 4, kH = 3;
  std::vector<uint8_t> rgba(kW * kH * 4, 0xff);
  auto png = kilnfile::bridge::EncodePngRgba8(kW, kH, rgba);
  CHECK(png.ok());
  static const char kSig[] = "\x89PNG\r\n\x1a\n";
  CHECK(png->size() > 8 && png->compare(0, 8, kSig, 8) == 0);
  CHECK(png->find("IHDR") == 12);  // first chunk
  CHECK(png->find("IEND") != std::string::npos);

  // A wrongly sized buffer must come back as an error, not a crash.
  auto bad = kilnfile::bridge::EncodePngRgba8(kW, kH, {rgba.data(), 5});
  CHECK(!bad.ok());
  std::cout << "bridge_png_test ok: " << png->size() << " PNG bytes; error path: "
            << bad.status() << std::endl;
  return 0;
}
