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
    "references.md"
)

INPUT_DIR="/Users/psshlykov/prog/mipt/cloud-nlb/nir_text"
OUTPUT_DIR="$INPUT_DIR/output"

# Создаем выходную директорию
mkdir -p "$OUTPUT_DIR"

# Временный файл с объединенным содержимым
TEMP_FILE="$OUTPUT_DIR/combined_temp.md"

# Очищаем временный файл
> "$TEMP_FILE"

# Добавляем файлы в правильном порядке
for file in "${FILES[@]}"; do
    if [ -f "$INPUT_DIR/$file" ]; then
        echo "Добавляем файл: $file"
        cat "$INPUT_DIR/$file" >> "$TEMP_FILE"
        echo -e "\n\n" >> "$TEMP_FILE"
    else
        echo "Файл не найден: $file"
    fi
done

# Создаем reference.docx с настройками ГОСТ
cat > "$OUTPUT_DIR/gost_template.md" << 'EOF'
---
mainfont: "Times New Roman"
fontsize: 14pt
papersize: a4
margin-left: 3cm
margin-right: 1.5cm
margin-top: 2cm
margin-bottom: 2cm
line-spacing: 1.5
geometry:
  - a4paper
  - left=3cm
  - right=1.5cm
  - top=2cm
  - bottom=2cm
---

EOF

# Конвертируем в DOCX с использованием форматирования ГОСТ
echo "Конвертируем в DOCX..."
pandoc "$TEMP_FILE" \
    --from markdown \
    --to docx \
    --reference-doc="$OUTPUT_DIR/gost_template.md" \
    --output="$OUTPUT_DIR/Unified_Document.docx" \
    --toc \
    --toc-depth=3 \
    --highlight-style=none

# Конвертируем в PDF если нужен
echo "Конвертируем в PDF..."
pandoc "$TEMP_FILE" \
    --from markdown \
    --to pdf \
    --pdf-engine=xelatex \
    --output="$OUTPUT_DIR/Unified_Document.pdf" \
    --toc \
    --toc-depth=3 \
    --variable=fontsize:14pt \
    --variable=mainfont:"Times New Roman" \
    --variable=geometry:"a4paper,left=3cm,right=1.5cm,top=2cm,bottom=2cm" \
    --variable=linespread:1.5 \
    --highlight-style=none

echo "Готово! Файлы сохранены в $OUTPUT_DIR:"
echo "- Unified_Document.docx"
echo "- Unified_Document.pdf"

# Очищаем временные файлы
rm -f "$TEMP_FILE" "$OUTPUT_DIR/gost_template.md"