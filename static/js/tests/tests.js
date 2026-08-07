// // #1, the blob URLS
// // -----------------------------
// function blobify(url) {
//     const evilHtml = `
//     <script>
//         fetch('${url}')
//             .then(response => response.text())
//             .then(html => {
//                 document.open();
//                 document.write(html);
//                 document.close();
//             })
//             .catch(error => {
//                 console.error("Failed to fetch and render:", error);
//                 document.body.innerHTML = '<h1>Failed to load content.</h1>';
//             });
//     </script>
// `;

//     const blob = new Blob([evilHtml], { type: "text/html" });

//     const iframe = document.createElement("iframe");
//     iframe.style.width = "100%";
//     iframe.style.height = "500px";
//     iframe.style.border = "1px solid black";
//     iframe.src = URL.createObjectURL(blob);

//     document.body.appendChild(iframe);
// }

function blobify(url) {
    // 1. Validate the URL to ensure it's a full URL.
    // This helps in creating the correct base URL.
    let targetUrl;
    try {
        targetUrl = new URL(url);
    } catch (e) {
        console.error("Invalid URL provided to blobify:", url);
        // Display an error in the DOM instead of creating a broken iframe
        const errorElement = document.createElement('div');
        errorElement.innerHTML = `<h1>Invalid URL</h1><p>The provided URL "${url}" is not valid.</p>`;
        document.body.appendChild(errorElement);
        return;
    }

    // 2. The payload, now with the essential <base> tag logic
    const evilHtml = `
    <script>
        // Get the full URL that was passed to the function
        const urlToFetch = '${targetUrl.href}';
        
        // The base href should be the origin of the URL (e.g., https://z-kit.net)
        const baseHref = '${targetUrl.origin}/';

        fetch(urlToFetch)
            .then(response => {
                if (!response.ok) {
                    throw new Error('Network response was not ok: ' + response.status);
                }
                return response.text();
            })
            .then(html => {
                // Create a <base> tag to fix all relative URLs
                const base = '<base href="' + baseHref + '">';
                
                // Inject the base tag right after the <head> tag.
                // This is more reliable than just prepending it.
                const fixedHtml = html.replace('<head>', '<head>' + base);

                // Render the fetched HTML
                document.open();
                document.write(fixedHtml);
                document.close();
            })
            .catch(error => {
                console.error("Failed to fetch and render:", error);
                // Display a more informative error inside the iframe
                document.body.innerHTML = '<h1>Failed to load content.</h1><p>' + error + '</p><p>This is often due to a CORS policy on the target website.</p>';
            });
    <\/script>
    `; // Escaping the closing script tag is good practice

    // Create the in-memory file (Blob)
    const blob = new Blob([evilHtml], { type: "text/html" });

    // Create an iframe and load the Blob into it
    const iframe = document.createElement("iframe");
    iframe.style.width = "100%";
    iframe.style.height = "500px";
    iframe.style.border = "1px solid black";
    // Add the sandbox attribute as a good security practice,
    // though it's not the cause of your current error.
    iframe.sandbox = 'allow-forms allow-modals allow-pointer-lock allow-popups allow-same-origin allow-scripts';
    iframe.src = URL.createObjectURL(blob);

    document.body.appendChild(iframe);
}

// --- HOW TO USE IT ---
// Call the function with the full URL you want to load.
// blobify('https://www.google.com');
// blobify('https://z-kit.net'); 
// blobify('http://info.cern.ch/'); // A site that allows framing