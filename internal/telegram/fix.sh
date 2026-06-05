sed -i '387a\
\tif msg == nil {\
\t\treturn\
\t}\
' internal/telegram/telegram.go
