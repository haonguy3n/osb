def user(name, uid = None, gid = None, home = "", shell = "/bin/sh", password = "", groups = []):
    if uid == None:
        uid = 0 if name == "root" else 1000
    if gid == None:
        gid = uid
    if not home:
        home = "/root" if uid == 0 else "/home/" + name
    return {
        "name": name,
        "uid": uid,
        "gid": gid,
        "home": home,
        "shell": shell,
        "password": password,
        "groups": list(groups),
    }
