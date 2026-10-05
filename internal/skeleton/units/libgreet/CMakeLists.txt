cmake_minimum_required(VERSION 3.16)
project(libgreet CXX)

set(CMAKE_CXX_STANDARD 17)
add_library(greet SHARED greet.cpp)
set_target_properties(greet PROPERTIES VERSION 1.0.0 SOVERSION 1)
install(TARGETS greet LIBRARY DESTINATION lib)
install(FILES greet.h DESTINATION include)
