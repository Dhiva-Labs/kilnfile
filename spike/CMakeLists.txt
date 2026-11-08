add_executable(bridge_png_test bridge_png_test.cc)
target_link_libraries(bridge_png_test PRIVATE kilnfile_bridge_cc)
add_test(NAME bridge_png_test COMMAND bridge_png_test)

# The M0 question: zip, xml and the kilnfile bridge in ONE executable.
add_executable(two_bindings_test two_bindings_test.cc)
target_link_libraries(two_bindings_test PRIVATE sb_zip sb_xml kilnfile_bridge_cc)
add_test(NAME two_bindings_test COMMAND two_bindings_test)
