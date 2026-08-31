//! parity-roundtrip — read a crtx v0.1 envelope JSON file at argv[1],
//! parse via `hop_top_stem::parse_envelope`, re-serialize via
//! `hop_top_stem::serialize_envelope`, write to stdout.
//!
//! Used by `tools/parity/runner.sh`. Run from `rs/` as:
//!
//! ```text
//! cargo run --example parity-roundtrip -- <path>
//! ```

use std::env;
use std::fs;
use std::io::{self, Write};
use std::process::ExitCode;

use hop_top_stem::{parse_envelope, serialize_envelope};

fn main() -> ExitCode {
    let args: Vec<String> = env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: parity-roundtrip <envelope.json>");
        return ExitCode::from(2);
    }

    let path = &args[1];
    let text = match fs::read_to_string(path) {
        Ok(t) => t,
        Err(err) => {
            eprintln!("parity-roundtrip: read {}: {}", path, err);
            return ExitCode::from(1);
        }
    };

    let env = match parse_envelope(&text) {
        Ok(e) => e,
        Err(err) => {
            eprintln!("parity-roundtrip: parse: {}", err);
            return ExitCode::from(1);
        }
    };

    let out = match serialize_envelope(&env) {
        Ok(s) => s,
        Err(err) => {
            eprintln!("parity-roundtrip: serialize: {}", err);
            return ExitCode::from(1);
        }
    };

    let stdout = io::stdout();
    let mut handle = stdout.lock();
    if let Err(err) = handle.write_all(out.as_bytes()) {
        eprintln!("parity-roundtrip: write: {}", err);
        return ExitCode::from(1);
    }
    ExitCode::SUCCESS
}
