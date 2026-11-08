cmake_minimum_required(VERSION 3.22)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
# Crubit's headers need clang (safe-bindings' own CMake files hard-code this).
if(NOT CMAKE_CXX_COMPILER)
  set(CMAKE_C_COMPILER clang)
  set(CMAKE_CXX_COMPILER clang++)
endif()

project(kilnfile VERSION 0.1.0 LANGUAGES CXX)
enable_testing()

include(FetchContent)

FetchContent_Declare(
  Corrosion
  # Same fork safe-bindings uses, until corrosion-rs/corrosion#666 is merged.
  GIT_REPOSITORY https://github.com/thunderseethe/corrosion
  GIT_TAG master
)
FetchContent_MakeAvailable(Corrosion)

# --- Rust side: one umbrella crate, one staticlib, one Crubit header ---------
# tools/vendor_bindings.sh folds safe-bindings' zip and xml (renamed sb_zip and
# sb_xml) and kilnfile_bridge into the crate `kilnfile_rs`.
set(KILN_VENDOR_DIR "${CMAKE_BINARY_DIR}/vendored")
execute_process(
  COMMAND "${CMAKE_CURRENT_SOURCE_DIR}/tools/vendor_bindings.sh" "${KILN_VENDOR_DIR}"
  RESULT_VARIABLE vendor_rc)
if(NOT vendor_rc EQUAL 0)
  message(FATAL_ERROR "tools/vendor_bindings.sh failed (${vendor_rc})")
endif()
set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS
  tools/vendor_bindings.sh bridge/Cargo.toml bridge/src/lib.rs)

corrosion_experimental_crubit(kilnfile_rs)
corrosion_import_crate(MANIFEST_PATH "${KILN_VENDOR_DIR}/kilnfile_rs/Cargo.toml")

# Abseil must be declared AFTER corrosion_import_crate: Crubit's own CMake
# declares abseil (20260526.0) while it is fetched there, and the first
# declaration wins. safe-bindings' xml pins 20250127.1, which has no
# absl::status_macros, and only works because of this ordering. We pin the same
# tag as Crubit so the result doesn't depend on that.
FetchContent_Declare(
  absl
  GIT_REPOSITORY https://github.com/abseil/abseil-cpp.git
  GIT_TAG 20260526.0
  FIND_PACKAGE_ARGS NAMES absl
)
FetchContent_MakeAvailable(absl)

# --- C++ wrappers (patched copies of safe-bindings' zip and xml) -------------
set(VENDORED_CPP "${KILN_VENDOR_DIR}/cpp")

add_library(sb_zip
  "${VENDORED_CPP}/zip/converters.cc"
  "${VENDORED_CPP}/zip/file.cc"
  "${VENDORED_CPP}/zip/read.cc"
  "${VENDORED_CPP}/zip/write.cc")
target_include_directories(sb_zip PUBLIC "${VENDORED_CPP}" "${VENDORED_CPP}/zip")
target_link_libraries(sb_zip PUBLIC kilnfile_rs
  absl::status absl::statusor absl::strings absl::span absl::status_macros)

add_library(sb_xml
  "${VENDORED_CPP}/xml/quick_reader.cc"
  "${VENDORED_CPP}/xml/quick_writer.cc"
  "${VENDORED_CPP}/xml/xmltree.cc")
target_include_directories(sb_xml PUBLIC "${VENDORED_CPP}" "${VENDORED_CPP}/xml")
target_compile_definitions(sb_xml PUBLIC ABSL_DEFINE_UNQUALIFIED_STATUS_MACROS)
target_link_libraries(sb_xml PUBLIC kilnfile_rs
  absl::die_if_null absl::status absl::statusor absl::strings absl::span
  absl::log absl::core_headers absl::status_macros)

add_subdirectory(bridge)
add_subdirectory(spike)
