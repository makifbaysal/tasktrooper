---
name: native-memory-safety-and-ffi
category: security
description: Use when the diff changes C, C++ or Objective-C, Rust unsafe blocks, Go unsafe or cgo, JNI, Python ctypes/cffi or C extensions, Swift unsafe pointers, C# unsafe or Marshal code, or a native plugin, parser or loader that sees untrusted bytes - bounds, integer overflow before allocation, unit confusion, lifetimes, uninitialized memory, format strings, FFI ownership and library search order
tech_stack: Native code & FFI
source: cloudflare/security-audit-skill (MIT) memory-safety, FFI/ABI and binary-loading attack classes, adapted to diff review; own wording; CWE and SEI CERT C/C++ cited by name only
---
# Native Memory Safety and FFI

## Overview

The hard exclusion on memory safety covers memory-safe languages. Native code is the opposite case: in C, C++, Objective-C, `unsafe` Rust, cgo, JNI and ctypes, one wrong length turns a packet, a save file, a mod, an image or a model file into a read or write outside the buffer. These bugs are in scope whenever untrusted bytes reach the code.

**Core principle:** re-derive every length, index and lifetime from the worst value the attacker can send — not from the value the tests use — and state the effect you can show (read, write, use-after-free, crash) without inflating it.

## 1. Who Sends the Bytes?

A finding needs a realistic untrusted source: a network peer, a file the user opens or downloads (save, replay, mod, asset, document, model), an IPC message from a less-trusted process, a plugin from a third party. A buffer bug reachable only from the program's own constant data, or from a developer's local CLI flag, is a bug, not a vulnerability.

## 2. Bounds (CWE-119, CWE-125, CWE-787)

```c
// ❌ len comes from the packet; dst holds 64 bytes
uint16_t len = read_u16(pkt);
memcpy(dst, pkt->data, len);

// ✅ check against the destination AND what the source actually holds
if (len > sizeof(dst) || len > pkt->remaining) return ERR_MALFORMED;
memcpy(dst, pkt->data, len);
```

Check both sides of every copy. Recompute the room left after a header, a prefix, alignment padding or a terminator. `strcpy`, `strcat`, `sprintf`, `gets` and `strncpy` (no terminator when it truncates) on input are findings by shape; `std::vector::operator[]` and `std::span` do not check, `.at()` does.

## 3. Integer Arithmetic Before Allocation (CWE-190, CWE-191, CWE-681)

```cpp
// ❌ both factors come from the wire: the 32-bit product wraps, the allocation is tiny,
//    and the loop still writes count records of rec bytes
uint32_t count = r.u32(), rec = r.u32();
uint8_t* buf = (uint8_t*)malloc(count * rec);
for (uint32_t i = 0; i < count; ++i) r.read(buf + (size_t)i * rec, rec);

// ✅ upper bounds, then checked multiplication in size_t
size_t bytes;
if (count > kMaxRecords || rec > kMaxRecordSize ||
    __builtin_mul_overflow((size_t)count, (size_t)rec, &bytes)) return Reject();
```

High-hit shapes: `a - b` where `b > a` can happen; a 64-bit length narrowed to `int` or `uint16_t`; a signed length that is negative passed as `size_t`; `-1` as a sentinel that becomes `SIZE_MAX`; an addition near the type's maximum. Rust: `checked_mul`/`checked_add`, or `try_into()` instead of `as` for narrowing.

## 4. Units (CWE-131)

Bytes vs elements vs UTF-16 code units vs pages; `sizeof(ptr)` where `sizeof(*ptr)` was meant; a length validated in one unit and used in another. A bounds check that uses the same wrong unit as the allocation is still wrong.

## 5. Lifetimes (CWE-416, CWE-415, CWE-825)

- A callback, timer, completion handler or observer that fires after its owner was freed — look at every error, cancel and close path.
- Iterator invalidation: `push_back`/`erase` on a container while iterating it; a reference into a vector kept across a resize.
- `c_str()` or `std::string_view` of a temporary; a pointer into a buffer that is reallocated.
- Double free on an error path that frees and then falls through to common cleanup.
- Rust `unsafe`: `slice::from_raw_parts(ptr, len)` with `len` from input, `get_unchecked`, `set_len` before initialization, `transmute` between unrelated types, a hand-written `unsafe impl Send/Sync`.

## 6. Uninitialized and Type-Confused Memory (CWE-457, CWE-843)

- A struct or buffer with padding or unwritten fields sent over the network, written to a file or hashed — it leaks stack or heap contents to a peer.
- A downcast chosen by a tag from the wire (`static_cast<Derived*>(base)` after `if (msg.kind == …)`) where the object's real type was never checked; a union read through the wrong member.

## 7. Format Strings (CWE-134)

`printf(user)`, `syslog(LOG_INFO, user)`, `NSLog(user)`, `String(format: user)` — a fixed format with the input as an argument is the fix.

## 8. FFI and ABI Boundaries

- **Ownership:** caller and callee disagree on who frees a buffer, or how long a pointer stays valid; a callback keeps a pointer to a Go, Java or Python object after the runtime moved or freed it.
- **cgo:** passing Go memory that contains Go pointers to C, or keeping a Go pointer in C after the call returns (both break the cgo pointer rules); `unsafe.Slice`/`unsafe.String` over a C buffer with a length from input.
- **JNI and ctypes:** `GetByteArrayElements`/`GetStringUTFChars` without the matching release; ctypes calls with no `argtypes`/`restype`, so 64-bit pointers are truncated to `int`.
- **Layout:** a struct declared differently on the two sides (packing, alignment, enum width), or a length passed in bytes where the other side expects elements.
- **Unwinding:** a Rust panic or C++ exception crossing an `extern "C"` boundary.

## 9. Library and Plugin Loading (CWE-426, CWE-427)

A process — especially one that runs elevated or starts automatically — loads a library, plugin or helper by bare name or relative path, from the working directory, a downloads folder or a user-writable plugin directory: `LoadLibrary("x.dll")`, `dlopen("libx.so")` with an attacker-influenced `LD_LIBRARY_PATH`, a plugin folder scanned without a signature check. A user loading their own plugin into their own process is not a boundary.

## How to Judge Without Running

This review never compiles or runs the code. Settle it from source: write the worst accepted input through the arithmetic, name the allocation and the access with `file:line`, and say which effect the trace shows — out-of-bounds read, out-of-bounds write, use-after-free, or only a clean rejection or abort. If the decisive fact is a build flag, architecture, allocator or a foreign library you cannot read, it is a needs-validation note (source-visibility-needs-validation), not a block.

## Severity Guide

An attacker-controlled out-of-bounds write or use-after-free reachable from the network or from an opened file → CRITICAL (HIGH when it needs a rare precondition). An out-of-bounds read that returns memory to the peer → HIGH. A crash of a shared process (a game server, a daemon) from one input → MEDIUM to HIGH as availability with shared impact (security-hard-exclusions-and-precedents §4). A crash of only the attacker's own client → not a finding.

## Common Mistakes

- Claiming code execution from an out-of-bounds read, or from a crash whose cause you did not trace.
- Reporting `strcpy` on a constant or a length-checked buffer.
- Reporting a memory leak (an un-freed `C.CString`, a missing `free`) as a vulnerability — it is a bug.
- Applying this skill to safe Go, Java, C#, Python or JavaScript — memory safety there is excluded.

## Red Flags

- A length, count or offset read from input and used in `memcpy`, `malloc`, an index or a loop bound with no check against the buffer.
- `count * size` or `a - b` before an allocation or copy, with no overflow check.
- `strcpy`, `sprintf`, `gets`, or `printf(var)`.
- `from_raw_parts`, `get_unchecked`, `set_len`, `transmute` inside a new `unsafe` block.
- `LoadLibrary`/`dlopen` with a relative name in a privileged or auto-started process.

## References (names and links only)

[SEI CERT C Coding Standard](https://wiki.sei.cmu.edu/confluence/display/c/SEI+CERT+C+Coding+Standard) · [SEI CERT C++](https://wiki.sei.cmu.edu/confluence/pages/viewpage.action?pageId=88046682) · [cgo pointer passing rules](https://pkg.go.dev/cmd/cgo#hdr-Passing_pointers) · [The Rustonomicon](https://doc.rust-lang.org/nomicon/) · CWE-119, 125, 131, 134, 190, 416, 426, 787, 843 · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
