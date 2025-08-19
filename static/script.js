console.log('Script loaded!')

function copyToClipboard() {
    const textarea_content = document.getElementById("output");
    textarea_content.select();
    navigator.clipboard.writeText(textarea_content.value);
    alert("Copied to clipboard!");
}

function deleteTemplate(templateId) {
    if (confirm('Are you sure you want to delete this template?')) {
        const form = document.createElement('form');
        form.method = 'POST';
        form.action = '/templates/delete';
        
        const input = document.createElement('input');
        input.type = 'hidden';
        input.name = 'id';
        input.value = templateId;
        
        form.appendChild(input);
        document.body.appendChild(form);
        form.submit();
    }
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