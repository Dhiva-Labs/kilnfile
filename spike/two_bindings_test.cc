// Milestone 0: the zip binding, the xml binding and the kilnfile bridge in one
// binary. Mimics the start of the Office pipeline: write a tiny OOXML-shaped
// zip, read it back, list its entries and parse the XML entry.
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

#include "bridge/kilnfile_bridge.h"
#include "read.h"
#include "write.h"
#include "xmltree.h"

#define CHECK(cond)                                                      \
  do {                                                                   \
    if (!(cond)) {                                                       \
      std::cerr << __FILE__ << ":" << __LINE__ << " CHECK failed: " #cond \
                << std::endl;                                            \
      std::exit(1);                                                      \
    }                                                                    \
  } while (0)

namespace zip = security::zip;
namespace xml = security::xml;

static std::string BuildDocx() {
  std::vector<uint8_t> px(2 * 2 * 4, 0x80);
  auto png = kilnfile::bridge::EncodePngRgba8(2, 2, px);
  CHECK(png.ok());

  auto w = zip::ZipWriter::FromBuffer("", false);
  CHECK(w.ok());
  auto add = [&](const std::string& name, const std::string& data) {
    CHECK(w->StartFile(name, zip::ZipWriterFileOptions()).ok());
    CHECK(w->WriteData(data).ok());
  };
  add("[Content_Types].xml",
      "<?xml version=\"1.0\"?><Types xmlns=\"http://schemas.openxmlformats.org/"
      "package/2006/content-types\"><Default Extension=\"png\" "
      "ContentType=\"image/png\"/></Types>");
  add("word/document.xml",
      "<w:document xmlns:w=\"urn:w\"><w:body><w:p><w:r><w:t>hello kiln"
      "</w:t></w:r></w:p></w:body></w:document>");
  add("word/media/image1.png", *png);
  auto data = w->Finish();
  CHECK(data.ok());
  return std::string(data->AsStringView());
}

int main() {
  const std::string docx = BuildDocx();

  auto archive = zip::ZipArchive::FromBuffer(docx);
  CHECK(archive.ok());
  auto n = archive->GetLength();
  CHECK(n.ok() && *n == 3);

  std::string document_xml;
  for (uintptr_t i = 0; i < *n; ++i) {
    auto f = archive->GetFileByIndex(i);
    CHECK(f.ok());
    auto name = f->GetFileName();
    CHECK(name.ok());
    std::cout << "entry " << i << ": " << *name << std::endl;
    if (*name == "word/document.xml") {
      auto bytes = f->GetFileData(1 << 20);
      CHECK(bytes.ok());
      document_xml = std::string(bytes->AsStringView());
    }
  }
  CHECK(!document_xml.empty());

  auto doc = xml::XmlDocument::Parse(document_xml);
  CHECK(doc.ok());
  auto root_name = doc->GetName(doc->Root());
  CHECK(root_name.ok() && *root_name == "document");
  auto text = doc->GetText(doc->Root());
  CHECK(text.ok() && *text == "hello kiln");
  std::cout << "two_bindings_test ok: root=" << *root_name << " text=\"" << *text
            << "\"" << std::endl;
  return 0;
}
