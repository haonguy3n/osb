cmake_minimum_required(VERSION 3.16)
project(hello-cpp CXX)

set(CMAKE_CXX_STANDARD 17)
find_package(ZLIB REQUIRED)
find_path(GREET_INCLUDE greet.h REQUIRED)
find_library(GREET_LIB greet REQUIRED)

add_executable(hello-cpp main.cpp)
target_include_directories(hello-cpp PRIVATE ${GREET_INCLUDE})
target_link_libraries(hello-cpp PRIVATE ZLIB::ZLIB ${GREET_LIB})
install(TARGETS hello-cpp RUNTIME DESTINATION bin)
