run_and_clear() {
    {
        "$@" </dev/null >/dev/null 2>&1 &
        local pid=$!
        disown "$pid" 2>/dev/null || true
    } 2>/dev/null

    # Immediately erase the line you just typed
    printf '\033[1A\r\033[2K'
}

