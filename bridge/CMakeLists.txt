add_library(kilnfile_bridge_cc kilnfile_bridge.cc kilnfile_bridge.h)
target_include_directories(kilnfile_bridge_cc PUBLIC "${PROJECT_SOURCE_DIR}")
target_link_libraries(kilnfile_bridge_cc PUBLIC kilnfile_rs
  absl::status absl::statusor absl::span)
