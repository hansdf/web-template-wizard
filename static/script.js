console.log('Script loaded!')

function copyToClipboard() {
    const textarea_content = document.getElementById("output");
    textarea_content.select();
    navigator.clipboard.writeText(textarea_content.value);
    alert("Copied to clipboard!");
}

function switchDarkMode() {
    document.body.classList.toggle("dark-mode");
    localStorage.setItem('darkMode', document.body.classList.contains('dark-mode'));
}

document.addEventListener('DOMContentLoaded', () => {
    const darkModeButton = document.getElementById('dark-mode');
    darkModeButton.addEventListener('click', switchDarkMode);
    
    if (localStorage.getItem('darkMode') === 'true') {
        document.body.classList.add('dark-mode');
    }

    const textarea = document.querySelector('textarea');
    textarea.style.height = 'auto';
    textarea.style.height = textarea.scrollHeight + 'px';
});