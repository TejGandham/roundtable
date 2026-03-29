#!/bin/sh
dd if=/dev/zero bs=1024 count=2048 2>/dev/null | tr '\0' 'A'
