use normalizer_tr::{AmbiguityPolicy, NormalizeOptions, Normalizer};
use std::sync::OnceLock;

static NORMALIZER: OnceLock<Option<Normalizer>> = OnceLock::new();
static mut OUT: Vec<u8> = Vec::new();

#[unsafe(no_mangle)]
pub extern "C" fn alloc(len: usize) -> *mut u8 {
    let mut buf = Vec::<u8>::with_capacity(len.max(1));
    let ptr = buf.as_mut_ptr();
    std::mem::forget(buf);
    ptr
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn dealloc(ptr: *mut u8, len: usize) {
    unsafe { drop(Vec::from_raw_parts(ptr, 0, len.max(1))) };
}

/// Normalizes UTF-8 text at ptr/len with the fallback policy. Returns the output length
/// (read it with out_ptr), or -1 when the input is not UTF-8 or normalization failed.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn normalize(ptr: *const u8, len: usize) -> i32 {
    let input = unsafe { std::slice::from_raw_parts(ptr, len) };
    let Ok(text) = std::str::from_utf8(input) else { return -1 };
    let Some(n) = NORMALIZER.get_or_init(|| Normalizer::new().ok()) else { return -1 };
    let mut opts = NormalizeOptions::default();
    opts.ambiguity_policy = AmbiguityPolicy::Fallback;
    match n.normalize(text, &opts) {
        Ok(r) => {
            let s = r.normalized_text().as_bytes().to_vec();
            let l = s.len() as i32;
            unsafe { OUT = s };
            l
        }
        Err(_) => -1,
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn out_ptr() -> *const u8 {
    unsafe { (*std::ptr::addr_of!(OUT)).as_ptr() }
}
