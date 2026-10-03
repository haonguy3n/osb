def merge_tasks(base, overrides):
    if not overrides:
        return list(base)
    result = list(base)
    for o in overrides:
        name = o.name
        idx = -1
        for i in range(len(result)):
            if result[i].name == name:
                idx = i
                break
        if getattr(o, "remove", False):
            if idx >= 0:
                result.pop(idx)
            continue
        if idx >= 0:
            result[idx] = o
        else:
            result.append(o)
    return result
