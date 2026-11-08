//! kilnfile_bridge: Rust capabilities that safe-bindings lacks, exposed to C++
//! through Crubit. Milestone 0 has one function: PNG encoding.
//!
//! Types are kept Crubit-friendly (plain structs and enums, `Result`, slices),
//! the same way the safe-bindings crates do it.

use image::{ExtendedColorType, ImageEncoder, codecs::png::PngEncoder};

/// Owned byte buffer handed to C++ (a minimal `Vec<u8>` wrapper).
#[repr(C)]
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct PngBytes(Vec<u8>);

impl PngBytes {
    pub fn as_ptr(&self) -> *const u8 {
        self.0.as_ptr()
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum BridgeErrorCode {
    InvalidArgument,
    Internal,
}

#[repr(C)]
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BridgeError {
    pub message: PngBytes,
    pub code: BridgeErrorCode,
}

impl BridgeError {
    fn new(message: String, code: BridgeErrorCode) -> Self {
        Self { message: PngBytes(message.into_bytes()), code }
    }
}

/// Encodes an RGBA8 buffer (`width * height * 4` bytes, row-major) as PNG.
pub fn encode_png_rgba8(width: u32, height: u32, rgba: &[u8]) -> Result<PngBytes, BridgeError> {
    let expected = (width as u64) * (height as u64) * 4;
    if width == 0 || height == 0 || expected != rgba.len() as u64 {
        return Err(BridgeError::new(
            format!("buffer is {} bytes, expected {} for {}x{} RGBA8", rgba.len(), expected, width, height),
            BridgeErrorCode::InvalidArgument,
        ));
    }
    let mut out = Vec::new();
    PngEncoder::new(&mut out)
        .write_image(rgba, width, height, ExtendedColorType::Rgba8)
        .map_err(|e| BridgeError::new(e.to_string(), BridgeErrorCode::Internal))?;
    Ok(PngBytes(out))
}
