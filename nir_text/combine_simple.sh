#!/bin/bash

# Порядок файлов для объединения
FILES=(
    "abstract.md"
    "intro.md"
    "definitions.md"
    "section_1_1_maglev.md"
    "section_1_2_unimog.md"
    "section_1_3_yandex.md"
    "section_1_4_requirements.md"
    "section_1_5_conclusion.md"
    "section_2_architecture.md"
    "section_3_dataplane.md"
    "section_4_agent.md"
    "section_5_cpl.md"
    "section_6_hc.md"
    "section_8_testing.md"
    "conclusion.md"
)

INPUT_DIR="/Users/psshlykov/prog/mipt/cloud-nlb/nir_text"
OUTPUT_DIR="$INPUT_DIR/output"
TEMP_FILE="$OUTPUT_DIR/combined_temp.md"

mkdir -p "$OUTPUT_DIR"
> "$TEMP_FILE"

# Добавляем файлы
for file in "${FILES[@]}"; do
    if [ -f "$INPUT_DIR/$file" ]; then
        echo "Добавляем файл: $file"
        cat "$INPUT_DIR/$file" >> "$TEMP_FILE"
        echo -e "\n\n---\n\n" >> "$TEMP_FILE"
    fi
done

echo "Создаем DOCX с базовым форматированием..."
pandoc "$TEMP_FILE" \
    --from markdown \
    --to docx \
    --output="$OUTPUT_DIR/Unified_Document.docx" \
    --toc

echo "Создаем PDF (если TeX установлен)..."
if command -v xelatex &> /dev/null; then
    pandoc "$TEMP_FILE" \
        --from markdown \
        --to pdf \
        --pdf-engine=xelatex \
        --output="$OUTPUT_DIR/Unified_Document.pdf" \
        --toc \
        --variable=geometry:"a4paper,left=3cm,right=1.5cm,top=2cm,bottom=2cm"
else
    echo "TeX не установлен, пропускаем PDF"
fi

echo "Готово! Проверьте файлы в $OUTPUT_DIR"
ls -la "$OUTPUT_DIR"