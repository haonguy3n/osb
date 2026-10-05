load("@core//classes/cmake.star", "cmake")

cmake(
    name = "libgreet",
    version = "1.0.0",
    source = "./libgreet",
)
