#!/usr/bin/env bash
set -euo pipefail

# Сборка НИР_БАЛАНСЕР_ОТЧЕТ.docx из markdown-файлов.
# Подробности в BUILD.md.

cd "$(dirname "$0")"

if [ -f "~\$Р_БАЛАНСЕР_ОТЧЕТ.docx" ]; then
    echo "Файл открыт в Word (найден lock-файл ~\$Р_БАЛАНСЕР_ОТЧЕТ.docx)."
    echo "Закрой документ и запусти скрипт ещё раз."
    exit 1
fi

if ! python3 -c "import docx" 2>/dev/null; then
    echo "Требуется python-docx. Устанавливаю..."
    pip3 install --quiet python-docx
fi

python3 build_nir_docx.py

echo
echo "Готово. Открой НИР_БАЛАНСЕР_ОТЧЕТ.docx в Word и нажми F9 на содержании, чтобы обновить TOC."
