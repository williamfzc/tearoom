//! Process entry point for Tearoom and its embedded terminal runtime.
fn main() {
    let args: Vec<_> = std::env::args_os().collect();
    let internal = std::path::Path::new(&args[0])
        .file_name()
        .is_some_and(|name| name == "tearoom-hc-internal");
    if internal
        || args
            .get(1)
            .is_some_and(|arg| arg == "serve" || arg == "hold")
    {
        std::process::exit(herdr_core::cli::main());
    }
    if args.get(1).is_some_and(|arg| arg == "hc") {
        if args
            .iter()
            .skip(2)
            .find(|arg| *arg != "--json")
            .is_some_and(|arg| arg == "update")
        {
            eprintln!("hc is built into Tearoom; update Tearoom instead");
            std::process::exit(1);
        }
        #[cfg(unix)]
        {
            use std::os::unix::process::CommandExt;
            let error = std::process::Command::new(&args[0])
                .arg0("tearoom-hc-internal")
                .args(&args[2..])
                .exec();
            eprintln!("cannot start embedded hc: {error}");
        }
        #[cfg(not(unix))]
        eprintln!("embedded hc is only supported on Unix");
        std::process::exit(1);
    }
    std::process::exit(tearoom::cli::run());
}
