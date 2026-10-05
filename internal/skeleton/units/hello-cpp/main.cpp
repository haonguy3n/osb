#include <iostream>
#include <string>
#include <vector>
#include <zlib.h>
#include <greet.h>

int main(int argc, char **argv) {
    std::string text = argc > 1 ? argv[1] : greet("osb") + ", " + greet("osb");
    std::vector<unsigned char> out(compressBound(text.size()));
    uLongf size = out.size();
    if (compress(out.data(), &size, reinterpret_cast<const Bytef *>(text.data()), text.size()) != Z_OK) {
        std::cerr << "compress failed\n";
        return 1;
    }
    std::cout << greet("world") << " - zlib " << zlibVersion() << " compressed "
              << text.size() << " bytes to " << size << "\n";
    return 0;
}
