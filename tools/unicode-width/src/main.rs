use unicode_width::UnicodeWidthChar;
fn main() {
    let mut ranges = Vec::new();
    let mut start = 0u32;
    let mut last = 0u32;
    let mut width = 255usize;
    for code in 0..=0x10ffff {
        let Some(c) = char::from_u32(code) else { continue };
        let w = c.width().unwrap_or(0);
        if w != width || code != last + 1 {
            if width != 1 && width != 255 { ranges.push(format!("[{},{},{}]", start,last,width)); }
            start = code; width = w;
        }
        last = code;
    }
    if width != 1 { ranges.push(format!("[{},{},{}]",start,last,width)); }
    println!("[{}]",ranges.join(","));
}
