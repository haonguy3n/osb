project(
    name = "{{NAME}}",
    version = "0.1.0",
    defaults = defaults(
        machine = "{{MACHINE}}",
        image = "my-image",
        distro = "{{DISTRO}}",
    ),
)
